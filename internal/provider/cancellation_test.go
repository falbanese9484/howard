package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func awaitProviderStop(t *testing.T, stopped <-chan struct{}) {
	t.Helper()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not stop within cancellation bound")
	}
}

func startCancelableProvider(p types.Provider, ctx context.Context, conn *types.BusConnector) <-chan struct{} {
	conn.Ctx = ctx
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		p.Chat(conn)
	}()
	return stopped
}

func replyToolCalls(w http.ResponseWriter, kind string, calls []types.ToolCall) {
	if kind == "ollama" {
		_ = json.NewEncoder(w).Encode(ollamaChatResponse{Message: types.Message{Role: "assistant", ToolCalls: calls}, Done: true})
		return
	}
	deltas := make([]any, 0, len(calls))
	for i, call := range calls {
		args, _ := json.Marshal(call.Function.Arguments)
		deltas = append(deltas, map[string]any{
			"index": i, "id": fmt.Sprintf("call-%d", i), "type": "function",
			"function": map[string]any{"name": call.Function.Name, "arguments": string(args)},
		})
	}
	data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": deltas}}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
}

func TestProvidersCanceledBeforeStart(t *testing.T) {
	for _, kind := range []string{"ollama", "openrouter"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				replyTestStream(w, kind, "unexpected", false)
			}))
			defer server.Close()
			p := newTestProvider(t, kind, &config.AgentConfig{Model: "test", ApiKey: "synthetic"}, setupToolTestProject(t), server.URL)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			conn := testConnector(p, "do not start")
			awaitProviderStop(t, startCancelableProvider(p, ctx, conn))
			if requests.Load() != 0 || len(conn.ResponseChan) != 0 || len(conn.ErrorChan) != 0 || len(conn.DoneChannel) != 0 {
				t.Fatal("already-canceled turn launched work or emitted output")
			}
		})
	}
}

func TestProvidersCancelBlockedModelStreamWithoutAffectingAnotherRun(t *testing.T) {
	for _, kind := range []string{"ollama", "openrouter"} {
		t.Run(kind, func(t *testing.T) {
			startedA, startedB := make(chan struct{}), make(chan struct{})
			requestStoppedA := make(chan struct{})
			releaseB, cleanup := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req recordedRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				if req.Model == "cancel-a" {
					close(startedA)
					select {
					case <-r.Context().Done():
						close(requestStoppedA)
					case <-cleanup:
					}
					return
				}
				close(startedB)
				select {
				case <-releaseB:
					replyTestStream(w, kind, "second run survived", false)
				case <-r.Context().Done():
					t.Error("canceling run A interrupted run B")
				case <-cleanup:
				}
			}))
			defer server.Close()
			defer close(cleanup)
			ctxA, cancelA := context.WithCancel(context.Background())
			defer cancelA()
			ctxB, cancelB := context.WithCancel(context.Background())
			defer cancelB()
			pa := newTestProvider(t, kind, &config.AgentConfig{Model: "cancel-a", ApiKey: "synthetic"}, setupToolTestProject(t), server.URL)
			pb := newTestProvider(t, kind, &config.AgentConfig{Model: "keep-b", ApiKey: "synthetic"}, setupToolTestProject(t), server.URL)
			ca, cb := testConnector(pa, "first"), testConnector(pb, "second")
			stoppedA := startCancelableProvider(pa, ctxA, ca)
			stoppedB := startCancelableProvider(pb, ctxB, cb)
			awaitProviderStop(t, startedA)
			awaitProviderStop(t, startedB)
			cancelA()
			cancelA() // Repeated requests are harmless.
			awaitProviderStop(t, stoppedA)
			awaitProviderStop(t, requestStoppedA)
			if len(ca.ResponseChan) != 0 || len(ca.DoneChannel) != 0 {
				t.Fatal("canceled blocked stream emitted content or completion")
			}
			select {
			case <-stoppedB:
				t.Fatal("other run stopped before its stream was released")
			default:
			}
			close(releaseB)
			awaitProviderStop(t, stoppedB)
			if len(cb.DoneChannel) != 1 || len(cb.ErrorChan) != 0 || len(cb.ResponseChan) != 1 {
				t.Fatal("independent run did not complete normally")
			}
			if out := <-cb.ResponseChan; out.Response != "second run survived" {
				t.Fatalf("other run output = %+v", out)
			}
		})
	}
}

func TestProvidersStoppedConsumersCanCancel(t *testing.T) {
	for _, kind := range []string{"ollama", "openrouter"} {
		for _, blocked := range []string{"content", "tool status", "error", "completion"} {
			t.Run(kind+"/"+blocked, func(t *testing.T) {
				replied := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(replied)
					switch blocked {
					case "error":
						w.WriteHeader(http.StatusInternalServerError)
					case "tool status":
						replyToolCalls(w, kind, []types.ToolCall{toolCall("write_file", map[string]any{"path": "must-not-exist", "content": "bad"})})
					case "content":
						replyTestStream(w, kind, "blocked content", false)
					case "completion":
						replyTestStream(w, kind, "", false)
					}
				}))
				defer server.Close()
				state := setupToolTestProject(t)
				p := newTestProvider(t, kind, &config.AgentConfig{Model: "test", ApiKey: "synthetic"}, state, server.URL)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				conn := testConnector(p, "stopped consumer")
				conn.ResponseChan = make(chan *types.ChatResponse)
				conn.ErrorChan = make(chan error)
				conn.DoneChannel = make(chan bool)
				stopped := startCancelableProvider(p, ctx, conn)
				awaitProviderStop(t, replied)
				// No receiver exists for the selected event. Regardless of whether
				// cancellation beats the send or interrupts it, Chat must join.
				select {
				case <-stopped:
					t.Fatal("provider returned without delivering its required event")
				case <-time.After(10 * time.Millisecond):
				}
				cancel()
				awaitProviderStop(t, stopped)
				if _, err := os.Stat(filepath.Join(state.root, "must-not-exist")); !os.IsNotExist(err) {
					t.Fatalf("tool launched after its consumer stopped: %v", err)
				}
			})
		}
	}
}

func TestProvidersCancelBetweenToolsPreservesCompletedEffects(t *testing.T) {
	for _, kind := range []string{"ollama", "openrouter"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				replyToolCalls(w, kind, []types.ToolCall{
					toolCall("write_file", map[string]any{"path": "completed.txt", "content": "keep me"}),
					toolCall("write_file", map[string]any{"path": "later.txt", "content": "must not run"}),
				})
			}))
			defer server.Close()
			state := setupToolTestProject(t)
			p := newTestProvider(t, kind, &config.AgentConfig{Model: "test", ApiKey: "synthetic"}, state, server.URL)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conn := testConnector(p, "two tools")
			conn.ResponseChan = make(chan *types.ChatResponse)
			stopped := startCancelableProvider(p, ctx, conn)
			select {
			case response := <-conn.ResponseChan:
				if response.Kind != types.ResponseKindToolStatus {
					t.Fatalf("first event = %+v", response)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("first tool was not announced")
			}
			// The first tool completes, then the next status blocks because the
			// consumer has stopped. Cancellation must prevent its side effect.
			deadline := time.Now().Add(3 * time.Second)
			for {
				data, err := os.ReadFile(filepath.Join(state.root, "completed.txt"))
				if err == nil && string(data) == "keep me" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("first side effect did not complete: %q, %v", data, err)
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			awaitProviderStop(t, stopped)
			if data, err := os.ReadFile(filepath.Join(state.root, "completed.txt")); err != nil || string(data) != "keep me" {
				t.Fatalf("cancellation discarded a completed effect: %q, %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(state.root, "later.txt")); !os.IsNotExist(err) {
				t.Fatalf("later tool executed: %v", err)
			}
			if requests.Load() != 1 || len(conn.DoneChannel) != 0 {
				t.Fatal("canceled turn continued to another model round or completed")
			}
		})
	}
}

func TestSendEventCanceledContextDoesNotUseReadyChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan string, 1)
	for range 100 {
		if sendEvent(ctx, ch, "late") {
			t.Fatal("already-canceled event was sent")
		}
	}
	if len(ch) != 0 {
		t.Fatal("canceled event was buffered")
	}
}

func TestSendEventUnblocksStoppedConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan string)
	entered, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		close(entered)
		if sendEvent(ctx, ch, "unread") {
			t.Error("send succeeded without a consumer")
		}
	}()
	awaitProviderStop(t, entered)
	cancel()
	awaitProviderStop(t, stopped)
}
