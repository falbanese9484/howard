package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// RunRequest describes one turn in a Session. IDs are caller-assigned opaque
// values; a new turn needs a new RunID even when SessionID is reused.
// Validation does not create a session, resolve paths, or enforce isolation.
type RunRequest struct {
	RunID         string `json:"run_id"`
	SessionID     string `json:"session_id"`
	Prompt        string `json:"prompt"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	WorkspaceRoot string `json:"workspace_root"`
}

func (r RunRequest) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"run_id", r.RunID}, {"session_id", r.SessionID},
		{"prompt", r.Prompt}, {"provider", r.Provider}, {"model", r.Model},
		{"workspace_root", r.WorkspaceRoot},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s must not be blank", field.name)
		}
	}
	if !filepath.IsAbs(r.WorkspaceRoot) {
		return fmt.Errorf("workspace_root must be absolute")
	}
	return nil
}

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCanceled  RunStatus = "canceled"
)

func (s RunStatus) Terminal() bool {
	return s == RunSucceeded || s == RunFailed || s == RunCanceled
}

// RunEvent records a lifecycle transition, not a provider token or tool event.
// Sequence starts at one and is contiguous within a run. Error is required only
// for failure. Cancellation is terminal only after execution has stopped, not
// merely when a cancellation request has been received.
type RunEvent struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Sequence  uint64    `json:"sequence"`
	Status    RunStatus `json:"status"`
	At        time.Time `json:"at"`
	Error     string    `json:"error,omitempty"`
}

func (e RunEvent) Validate() error {
	if strings.TrimSpace(e.RunID) == "" || strings.TrimSpace(e.SessionID) == "" {
		return fmt.Errorf("run_id and session_id must not be blank")
	}
	if e.Sequence == 0 || e.At.IsZero() {
		return fmt.Errorf("sequence and at must be set")
	}
	switch e.Status {
	case RunQueued, RunRunning, RunSucceeded, RunFailed, RunCanceled:
	default:
		return fmt.Errorf("unknown run status %q", e.Status)
	}
	if e.Status == RunFailed {
		if strings.TrimSpace(e.Error) == "" {
			return fmt.Errorf("failed event requires an error")
		}
	} else if e.Error != "" {
		return fmt.Errorf("only failed events may contain an error")
	}
	return nil
}

// ValidateAfter validates an ordered lifecycle stream incrementally. Pass nil
// for its first event. Callers must validate every event and retain the last
// accepted event; this function does not store state or deduplicate delivery.
func (e RunEvent) ValidateAfter(previous *RunEvent) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if previous == nil {
		if e.Sequence != 1 || e.Status != RunQueued {
			return fmt.Errorf("first event must be queued with sequence 1")
		}
		return nil
	}
	if err := previous.Validate(); err != nil {
		return fmt.Errorf("invalid previous event: %w", err)
	}
	if e.RunID != previous.RunID || e.SessionID != previous.SessionID {
		return fmt.Errorf("event identity must match previous event")
	}
	if e.Sequence <= previous.Sequence || e.Sequence-previous.Sequence != 1 {
		return fmt.Errorf("event sequence must increase by one")
	}
	if e.At.Before(previous.At) {
		return fmt.Errorf("event time must not precede previous event")
	}
	if previous.Status == RunQueued && (e.Status == RunRunning || e.Status == RunCanceled || e.Status == RunFailed) {
		return nil
	}
	if previous.Status == RunRunning && e.Status.Terminal() {
		return nil
	}
	return fmt.Errorf("invalid run transition %q to %q", previous.Status, e.Status)
}
