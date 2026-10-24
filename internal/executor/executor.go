// Package executor owns the lifecycle of one in-process turn. It does not store
// sessions, persist events, or guarantee in-flight cancellation.
package executor

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/types"
)

var (
	ErrInvalidRequest = errors.New("invalid run request")
	ErrRunActive      = errors.New("run ID already active")
	ErrSetup          = errors.New("execution setup failed")
	ErrProvider       = errors.New("provider execution failed")
	ErrNotStarted     = errors.New("execution canceled before start")
)

// Turn executes one prompt and returns only after all work and output have
// stopped. Output is always non-nil. Implementations must not retain or invoke
// output after returning.
type Turn interface {
	Run(context.Context, string, func(types.ChatResponse)) error
}

// Factory must construct fresh, independently owned turn state for each call.
// Setup must not launch a turn. Errors are sanitized by Executor.
type Factory func(domain.RunRequest) (Turn, error)

// Observer callbacks are synchronous and scoped to this Execute call. They must
// return promptly and must not panic. Different runs may call observers concurrently.
// Output contains provider text/tool status, never lifecycle or failure metadata.
type Observer struct {
	Lifecycle func(domain.RunEvent)
	Output    func(types.ChatResponse)
}

type Executor struct {
	factory Factory
	mu      sync.Mutex
	active  map[string]struct{}
}

func New(factory Factory) *Executor {
	return &Executor{factory: factory, active: make(map[string]struct{})}
}

// Execute rejects invalid, already-canceled, and duplicate active requests
// without emitting events. Accepted requests emit queued and exactly one terminal
// event before returning. Active-ID uniqueness is local to this Executor; IDs
// may be reused after return. SessionID correlates events, not conversation history.
func (e *Executor) Execute(ctx context.Context, request domain.RunRequest, observer Observer) error {
	if ctx == nil || request.Validate() != nil {
		return ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	if _, exists := e.active[request.RunID]; exists {
		e.mu.Unlock()
		return ErrRunActive
	}
	e.active[request.RunID] = struct{}{}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.active, request.RunID)
		e.mu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}

	var sequence uint64
	var lastTime time.Time
	emit := func(status domain.RunStatus, failure error) {
		// Compare wall time so ordering also survives JSON serialization, which
		// drops Go's monotonic clock component.
		at := time.Now().UTC()
		if at.Before(lastTime) {
			at = lastTime
		}
		lastTime = at
		sequence++
		event := domain.RunEvent{
			RunID: request.RunID, SessionID: request.SessionID,
			Sequence: sequence, Status: status, At: at,
		}
		if failure != nil {
			event.Error = failure.Error()
		}
		if observer.Lifecycle != nil {
			observer.Lifecycle(event)
		}
	}
	fail := func(err error) error {
		emit(domain.RunFailed, err)
		return err
	}
	emit(domain.RunQueued, nil)
	if ctx.Err() != nil {
		return fail(ErrNotStarted)
	}
	if e.factory == nil {
		return fail(ErrSetup)
	}
	turn, err := e.factory(request)
	if err != nil || turn == nil {
		return fail(ErrSetup)
	}
	if ctx.Err() != nil {
		return fail(ErrNotStarted)
	}
	emit(domain.RunRunning, nil)
	output := observer.Output
	if output == nil {
		output = func(types.ChatResponse) {}
	}
	if err := turn.Run(ctx, request.Prompt, output); err != nil {
		return fail(ErrProvider)
	}
	// A cancellation request alone is not an outcome. The turn's actual result
	// wins; reliable end-to-end cancellation is a subsequent slice.
	emit(domain.RunSucceeded, nil)
	return nil
}
