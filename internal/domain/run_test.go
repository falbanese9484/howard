package domain

import (
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRunRequestValidate(t *testing.T) {
	valid := RunRequest{"run-1", "session-1", "hello", "ollama", "model", t.TempDir()}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"RunID", "SessionID", "Prompt", "Provider", "Model", "WorkspaceRoot"} {
		t.Run(field, func(t *testing.T) {
			r := valid
			reflect.ValueOf(&r).Elem().FieldByName(field).SetString(" \t")
			if r.Validate() == nil {
				t.Fatal("accepted blank field")
			}
		})
	}
	valid.WorkspaceRoot = filepath.Join("relative", "root")
	if valid.Validate() == nil {
		t.Fatal("accepted relative workspace")
	}
}

func event(status RunStatus, seq uint64) RunEvent {
	e := RunEvent{RunID: "run-1", SessionID: "session-1", Sequence: seq,
		Status: status, At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	if status == RunFailed {
		e.Error = "provider failed"
	}
	return e
}

func TestRunLifecycle(t *testing.T) {
	for _, statuses := range [][]RunStatus{
		{RunQueued, RunRunning, RunSucceeded},
		{RunQueued, RunRunning, RunFailed},
		{RunQueued, RunRunning, RunCanceled},
		{RunQueued, RunFailed}, // Setup can fail before execution starts.
		{RunQueued, RunCanceled},
	} {
		var previous *RunEvent
		for i, status := range statuses {
			e := event(status, uint64(i+1))
			if err := e.ValidateAfter(previous); err != nil {
				t.Fatalf("%v: %v", statuses, err)
			}
			previous = &e
		}
	}
}

func TestAllRunTransitions(t *testing.T) {
	statuses := []RunStatus{RunQueued, RunRunning, RunSucceeded, RunFailed, RunCanceled}
	for _, from := range statuses {
		for _, to := range statuses {
			t.Run(string(from)+"/"+string(to), func(t *testing.T) {
				previous, next := event(from, 1), event(to, 2)
				want := from == RunQueued && (to == RunRunning || to == RunFailed || to == RunCanceled) ||
					from == RunRunning && (to == RunSucceeded || to == RunFailed || to == RunCanceled)
				if got := next.ValidateAfter(&previous) == nil; got != want {
					t.Fatalf("allowed = %v, want %v", got, want)
				}
			})
		}
	}
}

func TestRunEventRejectsMalformedEvents(t *testing.T) {
	for name, mutate := range map[string]func(*RunEvent){
		"run id":           func(e *RunEvent) { e.RunID = " " },
		"session id":       func(e *RunEvent) { e.SessionID = "" },
		"sequence":         func(e *RunEvent) { e.Sequence = 0 },
		"time":             func(e *RunEvent) { e.At = time.Time{} },
		"status":           func(e *RunEvent) { e.Status = "unknown" },
		"missing error":    func(e *RunEvent) { e.Status = RunFailed },
		"blank error":      func(e *RunEvent) { e.Status = RunFailed; e.Error = " " },
		"unexpected error": func(e *RunEvent) { e.Error = "failure" },
	} {
		t.Run(name, func(t *testing.T) {
			e := event(RunRunning, 2)
			mutate(&e)
			if e.Validate() == nil {
				t.Fatal("accepted malformed event")
			}
		})
	}
}

func TestRunEventRejectsBrokenStream(t *testing.T) {
	for name, mutate := range map[string]func(*RunEvent){
		"different run":     func(e *RunEvent) { e.RunID = "run-2" },
		"different session": func(e *RunEvent) { e.SessionID = "session-2" },
		"duplicate":         func(e *RunEvent) { e.Sequence = 1 },
		"gap":               func(e *RunEvent) { e.Sequence = 3 },
		"backward time":     func(e *RunEvent) { e.At = e.At.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			previous, next := event(RunQueued, 1), event(RunRunning, 2)
			mutate(&next)
			if next.ValidateAfter(&previous) == nil {
				t.Fatal("accepted broken stream")
			}
		})
	}
	for _, e := range []RunEvent{event(RunRunning, 1), event(RunQueued, 2), event(RunQueued, 0)} {
		if e.ValidateAfter(nil) == nil {
			t.Fatal("accepted invalid initial event")
		}
	}
	previous, next := event(RunQueued, math.MaxUint64), event(RunRunning, 1)
	if next.ValidateAfter(&previous) == nil {
		t.Fatal("accepted sequence wraparound")
	}
	previous = event("unknown", 1)
	if event(RunRunning, 2).ValidateAfter(&previous) == nil {
		t.Fatal("accepted invalid previous event")
	}
}

func TestRunEventJSONRoundTrip(t *testing.T) {
	want := event(RunFailed, 3)
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got RunEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}
