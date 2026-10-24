package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/executor"
	"github.com/falbanese9484/agent-harness/internal/provider"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

// This fixture models a body blocked in the HTTP transport until the request's
// context is canceled. Close is separately observed before lifecycle completion.
type cancelBody struct {
	ctx             context.Context
	reading, closed chan struct{}
	once            sync.Once
}

func (b *cancelBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.reading) })
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *cancelBody) Close() error { close(b.closed); return nil }

func TestExecutorCancellationClosesModelStreamBeforeTerminal(t *testing.T) {
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	reading, closed := make(chan struct{}), make(chan struct{})
	http.DefaultClient = &http.Client{Transport: runTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &cancelBody{ctx: req.Context(), reading: reading, closed: closed}}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := domain.RunRequest{RunID: "cancel-stream", SessionID: "session", Provider: "ollama", Model: "test", Prompt: "hello", WorkspaceRoot: t.TempDir()}
	var events []domain.RunEvent
	finished := make(chan error, 1)
	go func() {
		finished <- NewExecutor(&config.AgentConfig{Provider: "ollama"}, nil, provider.ReadOnly).Execute(ctx, req, executor.Observer{Lifecycle: func(event domain.RunEvent) {
			if event.Status.Terminal() {
				select {
				case <-closed:
				default:
					t.Error("terminal before HTTP body closed")
				}
			}
			events = append(events, event)
		}, Output: func(types.ChatResponse) { t.Error("unexpected canceled output") }})
	}()
	select {
	case <-reading:
	case <-time.After(5 * time.Second):
		t.Fatal("model stream did not start")
	}
	cancel()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled stream did not stop")
	}
	checkLifecycle(t, req, events, domain.RunQueued, domain.RunRunning, domain.RunCanceled)
}

func TestExecutorStoppedOutputConsumerCanCancel(t *testing.T) {
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	http.DefaultClient = &http.Client{Transport: runTransport(func(req *http.Request) (*http.Response, error) {
		body := strings.Repeat("{\"message\":{\"content\":\"chunk\"},\"done\":false}\n", 40)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := domain.RunRequest{RunID: "stopped-consumer", SessionID: "session", Provider: "ollama", Model: "test", Prompt: "hello", WorkspaceRoot: t.TempDir()}
	consumer := make(chan types.ChatResponse) // Deliberately no reader.
	entered := make(chan struct{})
	var once sync.Once
	var events []domain.RunEvent
	finished := make(chan error, 1)
	go func() {
		finished <- NewExecutor(&config.AgentConfig{Provider: "ollama"}, nil, provider.ReadOnly).Execute(ctx, req, executor.Observer{
			Lifecycle: func(event domain.RunEvent) { events = append(events, event) },
			Output: func(response types.ChatResponse) {
				once.Do(func() { close(entered) })
				select {
				case consumer <- response:
				case <-ctx.Done():
				}
			},
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("output did not begin")
	}
	cancel()
	select {
	case err := <-finished:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stopped consumer stranded execution")
	}
	checkLifecycle(t, req, events, domain.RunQueued, domain.RunRunning, domain.RunCanceled)
}
