package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/executor"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

type runTransport func(*http.Request) (*http.Response, error)

func (f runTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func checkLifecycle(t *testing.T, request domain.RunRequest, events []domain.RunEvent, statuses ...domain.RunStatus) {
	t.Helper()
	if len(events) != len(statuses) {
		t.Fatalf("events = %+v, want %v", events, statuses)
	}
	var previous *domain.RunEvent
	for i := range events {
		if err := events[i].ValidateAfter(previous); err != nil {
			t.Fatal(err)
		}
		if events[i].RunID != request.RunID || events[i].SessionID != request.SessionID || events[i].Status != statuses[i] {
			t.Fatalf("unexpected event %+v", events[i])
		}
		previous = &events[i]
	}
}

func TestExecutorOllamaIsolatedToolRoundTrips(t *testing.T) {
	// Replace only the HTTP boundary: real app construction, Ollama protocol,
	// streaming parsing, tool dispatch and workspace state execute below.
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })
	arrived := make(chan struct{}, 3)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	http.DefaultClient = &http.Client{Transport: runTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://localhost:11434/api/chat" {
			return nil, fmt.Errorf("unexpected endpoint %s", r.URL)
		}
		var req struct {
			Model    string          `json:"model"`
			Messages []types.Message `json:"messages"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		if len(req.Messages) < 2 || req.Messages[0].Role != "system" || !strings.Contains(req.Messages[0].Content, "original instruction") || len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" {
			return nil, fmt.Errorf("configuration snapshot lost: %+v", req)
		}
		var message types.Message
		if len(req.Messages) == 2 {
			if req.Messages[1].Role != "user" || req.Messages[1].Content != "prompt-"+req.Model {
				return nil, fmt.Errorf("wrong user prompt")
			}
			arrived <- struct{}{}
			select {
			case <-gate:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			message = types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{Function: types.ToolCallFunction{Name: "read_file", Arguments: map[string]any{"path": "owner.txt"}}}}}
		} else {
			if len(req.Messages) != 4 || req.Messages[3].Role != "tool" || !strings.Contains(req.Messages[3].Content, "owner-"+req.Model) {
				return nil, fmt.Errorf("mixed history/workspace: %+v", req.Messages)
			}
			message = types.Message{Role: "assistant", Content: "answer-" + req.Model}
		}
		data, err := json.Marshal(map[string]any{"message": message, "done": true})
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data) + "\n"))}, err
	})}
	cfg := &config.AgentConfig{
		Provider: "ollama", Model: "ignored", WorkspaceRoot: "ignored",
		SystemPrompt: config.SystemPromptConfig{Sections: []config.SystemPromptSection{{Content: []string{"original instruction"}}}},
		Tools:        []config.ToolConfig{{Name: "read_file", Parameters: []config.ParameterConfig{{ParameterName: "path"}}}},
	}
	exec := NewExecutor(cfg, nil)
	cfg.Provider = "changed"
	cfg.Tools[0].Name = "changed"
	cfg.Tools[0].Parameters[0].ParameterName = "changed"
	cfg.SystemPrompt.Sections[0].Content[0] = "changed"
	type result struct {
		request domain.RunRequest
		events  []domain.RunEvent
		answer  string
		tools   int
		err     error
	}
	run := func(req domain.RunRequest) result {
		got := result{request: req}
		got.err = exec.Execute(context.Background(), req, executor.Observer{
			Lifecycle: func(event domain.RunEvent) { got.events = append(got.events, event) },
			Output: func(response types.ChatResponse) {
				if got.events[len(got.events)-1].Status != domain.RunRunning {
					t.Error("output outside running state")
				}
				if response.Kind == types.ResponseKindToolStatus {
					got.tools++
				} else {
					got.answer += response.Response
				}
			},
		})
		return got
	}
	results := make(chan result, 2)
	var first domain.RunRequest
	for _, model := range []string{"a", "b"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "owner.txt"), []byte("owner-"+model), 0o600); err != nil {
			t.Fatal(err)
		}
		req := domain.RunRequest{RunID: model, SessionID: "shared-correlation", Provider: "ollama", Model: model, Prompt: "prompt-" + model, WorkspaceRoot: root}
		first = req
		go func() { results <- run(req) }()
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("independent runs did not reach HTTP boundary")
		}
	}
	release()
	check := func(got result) {
		t.Helper()
		if got.err != nil || got.answer != "answer-"+got.request.Model || got.tools != 1 {
			t.Fatalf("run result = %+v", got)
		}
		checkLifecycle(t, got.request, got.events, domain.RunQueued, domain.RunRunning, domain.RunSucceeded)
	}
	for range 2 {
		select {
		case got := <-results:
			check(got)
		case <-time.After(5 * time.Second):
			t.Fatal("run did not finish")
		}
	}
	// A new turn with the same session/run identity still has fresh history.
	go func() { results <- run(first) }()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("reused ID did not start fresh")
	}
	select {
	case got := <-results:
		check(got)
	case <-time.After(5 * time.Second):
		t.Fatal("reused ID did not finish")
	}
}

func TestExecutorSetupFailures(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		cfg  *config.AgentConfig
		req  domain.RunRequest
	}{
		{"nil config", nil, domain.RunRequest{Provider: "ollama", WorkspaceRoot: root}},
		{"provider mismatch", &config.AgentConfig{Provider: "ollama"}, domain.RunRequest{Provider: "openrouter", WorkspaceRoot: root}},
		{"missing root", &config.AgentConfig{Provider: "ollama"}, domain.RunRequest{Provider: "ollama", WorkspaceRoot: filepath.Join(root, "missing")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.RunID, tc.req.SessionID, tc.req.Prompt, tc.req.Model = "run", "session", "hello", "model"
			var events []domain.RunEvent
			err := NewExecutor(tc.cfg, nil).Execute(context.Background(), tc.req, executor.Observer{Lifecycle: func(event domain.RunEvent) { events = append(events, event) }})
			if err != executor.ErrSetup || events[len(events)-1].Error != executor.ErrSetup.Error() {
				t.Fatalf("setup error = %v, events = %+v", err, events)
			}
			checkLifecycle(t, tc.req, events, domain.RunQueued, domain.RunFailed)
		})
	}
}

func TestExecutorOllamaFailureIsSanitized(t *testing.T) {
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })
	http.DefaultClient = &http.Client{Transport: runTransport(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("private-path synthetic-secret upstream failure")
	})}
	req := domain.RunRequest{RunID: "failed", SessionID: "session", Provider: "ollama", Model: "model", Prompt: "hello", WorkspaceRoot: t.TempDir()}
	var events []domain.RunEvent
	err := NewExecutor(&config.AgentConfig{Provider: "ollama"}, nil).Execute(context.Background(), req, executor.Observer{
		Lifecycle: func(event domain.RunEvent) { events = append(events, event) },
		Output:    func(types.ChatResponse) { t.Error("provider error leaked as output") },
	})
	if err != executor.ErrProvider {
		t.Fatalf("error = %v", err)
	}
	checkLifecycle(t, req, events, domain.RunQueued, domain.RunRunning, domain.RunFailed)
	if events[len(events)-1].Error != executor.ErrProvider.Error() {
		t.Fatalf("raw failure exported: %+v", events)
	}
}
