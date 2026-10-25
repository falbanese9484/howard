package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/internal/web"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func toolCall(name string, args map[string]any) types.ToolCall {
	return types.ToolCall{Function: types.ToolCallFunction{Name: name, Arguments: args}}
}

func TestExecutionStatesIsolateToolsConcurrently(t *testing.T) {
	a, b := setupToolTestProject(t), setupToolTestProject(t)
	var wg sync.WaitGroup
	for i, state := range []*ExecutionState{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range 10 {
				path := fmt.Sprintf("nested/%d.txt", n)
				content := fmt.Sprintf("worker-%d", i)
				if _, err := state.executeToolCall(context.Background(), toolCall("write_file", map[string]any{"path": path, "content": content})); err != nil {
					t.Error(err)
					return
				}
				if _, err := state.executeToolCall(context.Background(), toolCall("edit_file", map[string]any{"path": path, "old_text": content, "new_text": content + " edited"})); err != nil {
					t.Error(err)
					return
				}
				out, err := state.executeToolCall(context.Background(), toolCall("read_file", map[string]any{"path": path}))
				if err != nil || out != content+" edited" {
					t.Errorf("worker %d read = %q, %v", i, out, err)
				}
			}
		}()
	}
	wg.Wait()
	for _, pair := range [][2]*ExecutionState{{a, b}, {b, a}} {
		state, other := pair[0], pair[1]
		if err := os.Symlink(other.root, filepath.Join(state.root, "escape")); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{filepath.Join(other.root, "nested/0.txt"), "../outside.txt", "escape/nested/0.txt"} {
			for _, name := range []string{"ls", "read_file", "edit_file", "write_file", "mkdir"} {
				_, err := state.executeToolCall(context.Background(), toolCall(name, map[string]any{"path": path, "content": "bad", "old_text": "worker", "new_text": "bad"}))
				if err == nil {
					t.Errorf("%s allowed escape %q", name, path)
				}
			}
		}
		out, err := state.executeToolCall(context.Background(), toolCall("bash", map[string]any{"command": "pwd -P"}))
		if err != nil || strings.TrimSpace(out) != state.root {
			t.Errorf("bash cwd = %q, %v; want %q", out, err, state.root)
		}
		out, err = state.executeToolCall(context.Background(), toolCall("ls", map[string]any{"path": "nested"}))
		if err != nil || !strings.Contains(out, "0.txt") {
			t.Errorf("ls = %q, %v", out, err)
		}
	}
}

func TestExecutionStatesOwnWebConfiguration(t *testing.T) {
	allowlist := []string{"allowed.example"}
	cfg := web.Config{Enabled: true, DomainAllowlist: allowlist, MaxFetchBytes: 1234}
	a, err := NewExecutionState(t.TempDir(), cfg, stubSearcher{results: []web.Result{{Title: "worker A"}}})
	if err != nil {
		t.Fatal(err)
	}
	b := setupToolTestProject(t)
	allowlist[0] = "changed.example"
	snapshot := a.web.Config()
	snapshot.DomainAllowlist[0] = "snapshot.example"
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call := toolCall("web_search", map[string]any{"query": "test"})
			out, err := a.executeToolCall(context.Background(), call)
			if err != nil || !strings.Contains(out, "worker A") {
				t.Errorf("enabled search = %q, %v", out, err)
			}
			if _, err := b.executeToolCall(context.Background(), call); !errors.Is(err, web.ErrDisabled) {
				t.Errorf("disabled search = %v", err)
			}
			if err := a.web.AuthorizeHost(context.Background(), "allowed.example", "test"); err != nil {
				t.Error(err)
			}
			if err := a.web.AuthorizeHost(context.Background(), "changed.example", "test"); err == nil {
				t.Error("configuration mutation widened allowlist")
			}
		}()
	}
	wg.Wait()
	if a.web.Config().DomainAllowlist[0] != "allowed.example" || a.web.Config().MaxFetchBytes != 1234 {
		t.Fatal("web config was not retained independently")
	}
}

func TestExecutionStateRejectsInvalidRoots(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"", file, file + "/missing"} {
		if _, err := NewExecutionState(root, web.Config{}, nil); err == nil {
			t.Errorf("accepted invalid root %q", root)
		}
	}
	var state *ExecutionState
	if _, err := state.executeToolCall(context.Background(), toolCall("bash", map[string]any{"command": "true"})); err == nil {
		t.Fatal("nil execution state dispatched a tool")
	}
}

func newTestProvider(t *testing.T, kind string, cfg *config.AgentConfig, state *ExecutionState, url string) types.Provider {
	t.Helper()
	cache := types.NewModelCache(3600)
	if kind == "ollama" {
		p := NewOllama(nil, cfg, cache, state)
		p.url = url
		return p
	}
	p, err := NewOpenRouter(nil, cfg, cache, state)
	if err != nil {
		t.Fatal(err)
	}
	p.url = url
	return p
}

func testConnector(p types.Provider, prompt string) *types.BusConnector {
	return &types.BusConnector{
		Ctx: context.Background(), Request: p.GenerateRequest(prompt),
		ResponseChan: make(chan *types.ChatResponse, 20), ErrorChan: make(chan error, 1), DoneChannel: make(chan bool, 1),
	}
}

type recordedRequest struct {
	Model    string            `json:"model"`
	Messages []recordedMessage `json:"messages"`
	Tools    []ollamaTool      `json:"tools"`
}

type recordedMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Synthetic streams exercise both actual provider loops, including dispatch.
func replyTestStream(w http.ResponseWriter, kind, text string, readTool bool) {
	if readTool {
		text = ""
	}
	if kind == "ollama" {
		msg := types.Message{Role: "assistant", Content: text}
		if readTool {
			msg.ToolCalls = []types.ToolCall{toolCall("read_file", map[string]any{"path": "owner.txt"})}
		}
		_ = json.NewEncoder(w).Encode(ollamaChatResponse{Message: msg, Done: true})
		return
	}
	delta := map[string]any{"content": text}
	if readTool {
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "read-owner", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"owner.txt"}`}}}
	}
	data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
}

func TestProvidersResetConversation(t *testing.T) {
	for _, kind := range []string{"ollama", "openrouter"} {
		for _, systemPrompt := range []string{"", "Keep this system instruction"} {
			t.Run(kind+"/"+systemPrompt, func(t *testing.T) {
				requests := make(chan recordedRequest, 3)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req recordedRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					requests <- req
					replyTestStream(w, kind, "answer", false)
				}))
				defer server.Close()
				cfg := &config.AgentConfig{Model: "initial", ApiKey: "synthetic"}
				if systemPrompt != "" {
					cfg.SystemPrompt = config.SystemPromptConfig{Sections: []config.SystemPromptSection{{Content: []string{systemPrompt}}}}
				}
				p := newTestProvider(t, kind, cfg, setupToolTestProject(t), server.URL)
				for _, prompt := range []string{"old prompt", "follow-up"} {
					conn := testConnector(p, prompt)
					p.Chat(conn)
					select {
					case err := <-conn.ErrorChan:
						t.Fatal(err)
					default:
					}
				}
				first, second := <-requests, <-requests
				if len(second.Messages) != len(first.Messages)+2 {
					t.Fatal("conversation did not retain earlier turn before reset")
				}
				p.SetModel("selected-model")
				if err := p.ResetConversation(); err != nil {
					t.Fatal(err)
				}
				conn := testConnector(p, "fresh prompt")
				p.Chat(conn)
				select {
				case err := <-conn.ErrorChan:
					t.Fatal(err)
				default:
				}
				next := <-requests
				if next.Model != "selected-model" || len(next.Messages) != len(first.Messages) {
					t.Fatalf("fresh request = %#v", next)
				}
				if systemPrompt != "" && next.Messages[0] != first.Messages[0] {
					t.Fatal("reset changed system prompt")
				}
				if last := next.Messages[len(next.Messages)-1]; last.Role != "user" || last.Content != "fresh prompt" {
					t.Fatalf("fresh user message = %#v", last)
				}
			})
		}
	}
}

func TestProvidersResetRejectsActiveTurn(t *testing.T) {
	ollama := &Ollama{messages: []types.Message{{Role: "user", Content: "keep"}}}
	openrouter := &OpenRouter{messages: []openRouterMessage{{Role: "user", Content: "keep"}}}
	for _, test := range []struct {
		provider types.Provider
		state    *conversationState
	}{
		{ollama, &ollama.conversationState},
		{openrouter, &openrouter.conversationState},
	} {
		test.state.turnMu.Lock()
		err := test.provider.ResetConversation()
		test.state.turnMu.Unlock()
		if !errors.Is(err, ErrConversationBusy) {
			t.Fatalf("reset while active = %v", err)
		}
	}
	if len(ollama.messages) != 1 || len(openrouter.messages) != 1 {
		t.Fatal("busy reset discarded history")
	}
}

func TestProvidersIsolateHistoryModelConfigAndTools(t *testing.T) {
	for _, kind := range []string{"ollama", "openrouter"} {
		t.Run(kind, func(t *testing.T) {
			requests := make(chan recordedRequest, 20)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req recordedRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- req
				last := req.Messages[len(req.Messages)-1]
				replyTestStream(w, kind, last.Content, last.Role == "user")
			}))
			defer server.Close()
			cfg := &config.AgentConfig{Model: "initial", ApiKey: "synthetic", Tools: []config.ToolConfig{{Name: "read_file", Parameters: []config.ParameterConfig{{ParameterName: "path"}}}}}
			states := []*ExecutionState{setupToolTestProject(t), setupToolTestProject(t)}
			providers := make([]types.Provider, 2)
			for i, state := range states {
				owner := fmt.Sprintf("owner-%d", i)
				if err := os.WriteFile(filepath.Join(state.root, "owner.txt"), []byte(owner), 0o600); err != nil {
					t.Fatal(err)
				}
				providers[i] = newTestProvider(t, kind, cfg, state, server.URL)
			}
			cfg.Model = "mutated"
			cfg.Tools[0].Name = "bash"
			cfg.Tools[0].Parameters[0].ParameterName = "command"
			request := providers[0].GenerateRequest("discarded")
			request.Tools[0].Parameters[0].ParameterName = "request-mutated"
			providers[0].SetModel("model-0")
			var wg sync.WaitGroup
			for i, p := range providers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for turn := range 2 {
						conn := testConnector(p, fmt.Sprintf("prompt-%d-%d", i, turn))
						p.Chat(conn)
						select {
						case err := <-conn.ErrorChan:
							t.Error(err)
						default:
						}
						if len(conn.DoneChannel) != 1 {
							t.Error("turn did not complete")
						}
						var out string
						for len(conn.ResponseChan) > 0 {
							r := <-conn.ResponseChan
							if r.Kind == types.ResponseKindAssistant {
								out += r.Response
							}
						}
						if out != fmt.Sprintf("owner-%d", i) {
							t.Errorf("worker %d output = %q", i, out)
						}
					}
				}()
			}
			wg.Wait()
			if len(requests) != 8 {
				t.Fatalf("requests = %d, want 8", len(requests))
			}
			for len(requests) > 0 {
				req := <-requests
				owner := 0
				if req.Model == "initial" {
					owner = 1
				} else if req.Model != "model-0" {
					t.Errorf("unexpected model %q", req.Model)
				}
				if req.Tools[0].Function.Name != "read_file" || req.Tools[0].Function.Parameters.Required[0] != "path" {
					t.Errorf("aliased tool config: %#v", req.Tools)
				}
				for _, msg := range req.Messages {
					if msg.Role == "user" && !strings.HasPrefix(msg.Content, fmt.Sprintf("prompt-%d-", owner)) {
						t.Errorf("mixed user history: %#v", req.Messages)
					}
					if msg.Role == "tool" && msg.Content != fmt.Sprintf("owner-%d", owner) {
						t.Errorf("mixed tool history: %#v", req.Messages)
					}
				}
			}
		})
	}
}

func TestProvidersRejectOverlappingTurnsAndSnapshotModel(t *testing.T) {
	for _, kind := range []string{"ollama", "openrouter"} {
		t.Run(kind, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			models := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req recordedRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				models <- req.Model
				last := req.Messages[len(req.Messages)-1]
				if last.Role == "user" {
					close(entered)
					<-release
				}
				replyTestStream(w, kind, "done", last.Role == "user")
			}))
			defer server.Close()
			defer unblock()
			p := newTestProvider(t, kind, &config.AgentConfig{Model: "before", ApiKey: "synthetic"}, setupToolTestProject(t), server.URL)
			conn := testConnector(p, "first")
			finished := make(chan struct{})
			go func() { p.Chat(conn); close(finished) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("provider did not start")
			}
			p.SetModel("after")
			second := testConnector(p, "overlap")
			p.Chat(second)
			if err := <-second.ErrorChan; !errors.Is(err, ErrConversationBusy) {
				t.Fatalf("overlapping Chat = %v", err)
			}
			unblock()
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("provider did not finish")
			}
			if len(conn.DoneChannel) != 1 || len(models) != 2 {
				t.Fatal("expected one completed tool round-trip")
			}
			for range 2 {
				if model := <-models; model != "before" {
					t.Errorf("active turn changed model to %q", model)
				}
			}
			if p.GenerateRequest("next").Model != "after" {
				t.Fatal("model change not applied to subsequent requests")
			}
		})
	}
}
