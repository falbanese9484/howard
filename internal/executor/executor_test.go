package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/types"
)

type turnFunc func(context.Context, string, func(types.ChatResponse)) error

func (f turnFunc) Run(ctx context.Context, prompt string, output func(types.ChatResponse)) error {
	return f(ctx, prompt, output)
}

func request(t *testing.T) domain.RunRequest {
	t.Helper()
	return domain.RunRequest{RunID: "run", SessionID: "session", Prompt: "hello", Provider: "ollama", Model: "test", WorkspaceRoot: t.TempDir()}
}

func validateStream(t *testing.T, req domain.RunRequest, events []domain.RunEvent, statuses ...domain.RunStatus) {
	t.Helper()
	var previous *domain.RunEvent
	var got []domain.RunStatus
	terminals := 0
	for i := range events {
		event := &events[i]
		if err := event.ValidateAfter(previous); err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		if event.RunID != req.RunID || event.SessionID != req.SessionID {
			t.Fatalf("wrong identity: %+v", event)
		}
		if event.Status.Terminal() {
			terminals++
		}
		got = append(got, event.Status)
		previous = event
	}
	if !reflect.DeepEqual(got, statuses) || (len(events) > 0 && terminals != 1) {
		t.Fatalf("statuses = %v, want %v; terminal count %d", got, statuses, terminals)
	}
}

func TestExecuteOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		invalid    bool
		setupError bool
		turnError  bool
		want       error
		statuses   []domain.RunStatus
	}{
		{name: "success", statuses: []domain.RunStatus{domain.RunQueued, domain.RunRunning, domain.RunSucceeded}},
		{name: "invalid", invalid: true, want: ErrInvalidRequest},
		{name: "setup failure", setupError: true, want: ErrSetup, statuses: []domain.RunStatus{domain.RunQueued, domain.RunFailed}},
		{name: "provider failure", turnError: true, want: ErrProvider, statuses: []domain.RunStatus{domain.RunQueued, domain.RunRunning, domain.RunFailed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := request(t)
			if tc.invalid {
				req.Prompt = " "
			}
			setup, turns := 0, 0
			exec := New(func(got domain.RunRequest) (Turn, error) {
				setup++
				if got != req {
					t.Fatalf("factory request = %+v", got)
				}
				if tc.setupError {
					return nil, errors.New("secret credential and private path")
				}
				return turnFunc(func(ctx context.Context, prompt string, output func(types.ChatResponse)) error {
					turns++
					if prompt != req.Prompt {
						t.Fatal("wrong prompt")
					}
					output(types.ChatResponse{Kind: types.ResponseKindAssistant, Response: "answer"})
					if tc.turnError {
						return errors.New("secret upstream response")
					}
					return nil
				}), nil
			})
			var events []domain.RunEvent
			outputs := 0
			err := exec.Execute(context.Background(), req, Observer{
				Lifecycle: func(event domain.RunEvent) { events = append(events, event) },
				Output: func(response types.ChatResponse) {
					if events[len(events)-1].Status != domain.RunRunning || response.Response != "answer" {
						t.Fatal("output outside running state")
					}
					outputs++
				},
			})
			if err != tc.want {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			validateStream(t, req, events, tc.statuses...)
			for _, event := range events {
				if event.Status == domain.RunFailed && event.Error != tc.want.Error() {
					t.Fatalf("unsanitized error: %q", event.Error)
				}
			}
			if tc.invalid && setup != 0 || tc.setupError && turns != 0 || turns != outputs {
				t.Fatalf("setup=%d turns=%d outputs=%d", setup, turns, outputs)
			}
		})
	}
}

func TestCancellationBeforeLaunch(t *testing.T) {
	for _, stage := range []string{"before acceptance", "queued", "setup"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := request(t)
			setups := 0
			exec := New(func(domain.RunRequest) (Turn, error) {
				setups++
				cancel()
				return turnFunc(func(context.Context, string, func(types.ChatResponse)) error {
					t.Fatal("canceled request launched work")
					return nil
				}), nil
			})
			if stage == "before acceptance" {
				cancel()
			}
			var events []domain.RunEvent
			err := exec.Execute(ctx, req, Observer{Lifecycle: func(event domain.RunEvent) {
				events = append(events, event)
				if stage == "queued" {
					cancel()
				}
			}})
			if stage == "before acceptance" {
				if err != context.Canceled {
					t.Fatal(err)
				}
				validateStream(t, req, events)
			} else {
				if err != ErrNotStarted {
					t.Fatal(err)
				}
				validateStream(t, req, events, domain.RunQueued, domain.RunFailed)
			}
			if stage != "setup" && setups != 0 {
				t.Fatal("unexpected setup")
			}
		})
	}
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for execution")
	}
}

func TestActiveIDsAndIndependentRuns(t *testing.T) {
	a, b := request(t), request(t)
	b.RunID, b.SessionID, b.Prompt, b.Model = "other", "other-session", "other-prompt", "other-model"
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	exec := New(func(req domain.RunRequest) (Turn, error) {
		return turnFunc(func(_ context.Context, prompt string, output func(types.ChatResponse)) error {
			if req.RunID == a.RunID {
				close(started)
				<-release
			}
			output(types.ChatResponse{Response: prompt + req.Model + req.WorkspaceRoot})
			return nil
		}), nil
	})
	var eventsA []domain.RunEvent
	var outputA string
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if err := exec.Execute(context.Background(), a, Observer{
			Lifecycle: func(event domain.RunEvent) { eventsA = append(eventsA, event) },
			Output:    func(response types.ChatResponse) { outputA += response.Response },
		}); err != nil {
			t.Error(err)
		}
	}()
	await(t, started)
	if err := exec.Execute(context.Background(), a, Observer{Lifecycle: func(domain.RunEvent) {
		t.Error("duplicate emitted event")
	}}); err != ErrRunActive {
		t.Fatalf("duplicate error = %v", err)
	}
	var eventsB []domain.RunEvent
	var outputB string
	if err := exec.Execute(context.Background(), b, Observer{
		Lifecycle: func(event domain.RunEvent) { eventsB = append(eventsB, event) },
		Output:    func(response types.ChatResponse) { outputB += response.Response },
	}); err != nil {
		t.Fatal(err)
	}
	validateStream(t, b, eventsB, domain.RunQueued, domain.RunRunning, domain.RunSucceeded)
	if outputB != b.Prompt+b.Model+b.WorkspaceRoot {
		t.Fatal("run B output mixed")
	}
	// Release A without closing twice in cleanup.
	release <- struct{}{}
	await(t, finished)
	validateStream(t, a, eventsA, domain.RunQueued, domain.RunRunning, domain.RunSucceeded)
	if outputA != a.Prompt+a.Model+a.WorkspaceRoot {
		t.Fatal("run A output mixed")
	}
	// Completed IDs are not a replay/idempotency store. Reuse B with a fresh turn.
	if err := exec.Execute(context.Background(), b, Observer{}); err != nil {
		t.Fatalf("completed ID retained: %v", err)
	}
}

type fakeProvider struct {
	chat func(*types.BusConnector)
}

func (p fakeProvider) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{Prompt: prompt}
}
func (p fakeProvider) Chat(conn *types.BusConnector) { p.chat(conn) }

func TestProviderAdapterDrainsAndWaitsForReturn(t *testing.T) {
	for _, outcome := range []string{"done", "error", "missing", "output error"} {
		t.Run(outcome, func(t *testing.T) {
			req := request(t)
			signaled, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			exec := New(func(domain.RunRequest) (Turn, error) {
				return NewProviderTurn(fakeProvider{chat: func(conn *types.BusConnector) {
					if conn.Ctx != ctx || conn.Request.Prompt != req.Prompt {
						t.Error("adapter lost context or prompt")
					}
					switch outcome {
					case "done":
						conn.DoneChannel <- true
					case "error":
						conn.ErrorChan <- errors.New("private provider error")
					case "output error":
						conn.ResponseChan <- &types.ChatResponse{Kind: types.ResponseKindError, Response: "private error"}
						conn.DoneChannel <- true
					}
					close(signaled)
					<-release
					// More output than the channel buffer, including after completion signal.
					for i := 0; i < 40; i++ {
						conn.ResponseChan <- &types.ChatResponse{Kind: types.ResponseKindAssistant, Response: "x"}
					}
				}}), nil
			})
			var events []domain.RunEvent
			var result error
			count := 0
			go func() {
				defer close(finished)
				result = exec.Execute(ctx, req, Observer{
					Lifecycle: func(event domain.RunEvent) { events = append(events, event) },
					Output: func(response types.ChatResponse) {
						if response.Response != "x" || events[len(events)-1].Status != domain.RunRunning {
							t.Error("raw error or post-terminal output")
						}
						count++
					},
				})
			}()
			await(t, signaled)
			cancel()
			select {
			case <-finished:
				t.Fatal("finalized before provider stopped")
			default:
			}
			release <- struct{}{}
			await(t, finished)
			terminal := domain.RunFailed
			if outcome == "done" {
				terminal = domain.RunSucceeded
				if result != nil {
					t.Fatal(result)
				}
			} else if result != ErrProvider {
				t.Fatalf("error = %v", result)
			}
			validateStream(t, req, events, domain.RunQueued, domain.RunRunning, terminal)
			if count != 40 {
				t.Fatalf("delivered %d chunks, want 40", count)
			}
		})
	}
}
