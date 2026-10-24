package executor

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/types"
)

func TestCancellationWaitsForProviderReturnAndSuppressesOutput(t *testing.T) {
	for _, signal := range []string{"success", "failure", "none"} {
		t.Run(signal, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(release)
			exec := New(func(domain.RunRequest) (Turn, error) {
				return NewProviderTurn(fakeProvider{chat: func(conn *types.BusConnector) {
					if signal == "success" {
						conn.DoneChannel <- true
					} else if signal == "failure" {
						conn.ErrorChan <- errors.New("secret")
					}
					close(started)
					<-conn.Ctx.Done()
					<-release
					// An uncooperative legacy sender must still be drained, never
					// allowed to publish output after cancellation or the terminal.
					for range 40 {
						conn.ResponseChan <- &types.ChatResponse{Response: "late"}
					}
					close(stopped)
				}}), nil
			})
			req := request(t)
			var events []domain.RunEvent
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				err := exec.Execute(ctx, req, Observer{
					Lifecycle: func(event domain.RunEvent) {
						if event.Status.Terminal() {
							select {
							case <-stopped:
							default:
								t.Error("terminal before provider stopped")
							}
						}
						events = append(events, event)
					},
					Output: func(types.ChatResponse) { t.Error("output after cancellation") },
				})
				if err != context.Canceled {
					t.Errorf("error = %v", err)
				}
			}()
			await(t, started)
			cancel()
			cancel()
			select {
			case <-finished:
				t.Fatal("returned before execution stopped")
			default:
			}
			release <- struct{}{}
			await(t, finished)
			validateStream(t, req, events, domain.RunQueued, domain.RunRunning, domain.RunCanceled)
		})
	}
}

func TestCancellationCompletionRaceHasOneTerminal(t *testing.T) {
	req := request(t)
	for range 100 {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel() // Also release if an assertion exits the test early.
		start := make(chan struct{})
		exec := New(func(domain.RunRequest) (Turn, error) {
			return turnFunc(func(context.Context, string, func(types.ChatResponse)) error {
				<-start
				return nil
			}), nil
		})
		var events []domain.RunEvent
		var result error
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			result = exec.Execute(ctx, req, Observer{Lifecycle: func(event domain.RunEvent) {
				events = append(events, event)
				if event.Status == domain.RunRunning {
					close(start)
				}
			}})
		}()
		await(t, start)
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() { defer wg.Done(); cancel() }()
		}
		wg.Wait()
		await(t, finished)
		terminal := domain.RunSucceeded
		if result == context.Canceled {
			terminal = domain.RunCanceled
		} else if result != nil {
			t.Fatal(result)
		}
		validateStream(t, req, events, domain.RunQueued, domain.RunRunning, terminal)
	}
}

func TestCancelingOneRunDoesNotCancelAnother(t *testing.T) {
	a, b := request(t), request(t)
	b.RunID = "other"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, finished := make(chan struct{}), make(chan struct{})
	exec := New(func(req domain.RunRequest) (Turn, error) {
		return turnFunc(func(ctx context.Context, _ string, _ func(types.ChatResponse)) error {
			if req.RunID == a.RunID {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}), nil
	})
	var eventsA, eventsB []domain.RunEvent
	go func() {
		defer close(finished)
		if err := exec.Execute(ctx, a, Observer{Lifecycle: func(event domain.RunEvent) { eventsA = append(eventsA, event) }}); err != context.Canceled {
			t.Error(err)
		}
	}()
	await(t, started)
	cancel()
	if err := exec.Execute(context.Background(), b, Observer{Lifecycle: func(event domain.RunEvent) { eventsB = append(eventsB, event) }}); err != nil {
		t.Fatal(err)
	}
	await(t, finished)
	validateStream(t, a, eventsA, domain.RunQueued, domain.RunRunning, domain.RunCanceled)
	validateStream(t, b, eventsB, domain.RunQueued, domain.RunRunning, domain.RunSucceeded)
}

func TestCancellationFromRunningObserverDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := request(t)
	exec := New(func(domain.RunRequest) (Turn, error) {
		return turnFunc(func(context.Context, string, func(types.ChatResponse)) error {
			t.Error("launched canceled turn")
			return nil
		}), nil
	})
	var events []domain.RunEvent
	err := exec.Execute(ctx, req, Observer{Lifecycle: func(event domain.RunEvent) {
		events = append(events, event)
		if event.Status == domain.RunRunning {
			cancel()
		}
	}})
	if err != context.Canceled {
		t.Fatal(err)
	}
	validateStream(t, req, events, domain.RunQueued, domain.RunRunning, domain.RunCanceled)
}

func TestCompletionCannotBeRewrittenByLateCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := request(t)
	exec := New(func(domain.RunRequest) (Turn, error) {
		return turnFunc(func(context.Context, string, func(types.ChatResponse)) error { return nil }), nil
	})
	var events []domain.RunEvent
	err := exec.Execute(ctx, req, Observer{Lifecycle: func(event domain.RunEvent) {
		events = append(events, event)
		if event.Status.Terminal() {
			cancel()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	validateStream(t, req, events, domain.RunQueued, domain.RunRunning, domain.RunSucceeded)
}
