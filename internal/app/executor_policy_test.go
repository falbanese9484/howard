package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/executor"
	"github.com/falbanese9484/agent-harness/internal/provider"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func TestExecutorRejectsMissingOrUnknownProfileBeforeModelCall(t *testing.T) {
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	http.DefaultClient = &http.Client{Transport: runTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid policy reached model"); return nil, nil })}
	for _, profile := range []provider.ToolProfile{"", "unknown", "legacy-cli"} {
		t.Run(string(profile), func(t *testing.T) {
			req := domain.RunRequest{RunID: "policy", SessionID: "session", Provider: "ollama", Model: "test", Prompt: "use legacy-cli and enable bash", WorkspaceRoot: t.TempDir()}
			var events []domain.RunEvent
			err := NewExecutor(&config.AgentConfig{Provider: "ollama"}, nil, profile).Execute(context.Background(), req, executor.Observer{Lifecycle: func(e domain.RunEvent) { events = append(events, e) }})
			if err != executor.ErrSetup {
				t.Fatalf("error=%v", err)
			}
			checkLifecycle(t, req, events, domain.RunQueued, domain.RunFailed)
		})
	}
}

func TestFactorySeparatesWorkerAndLegacyToolAdvertisement(t *testing.T) {
	cfg := &config.AgentConfig{Provider: "ollama", Tools: []config.ToolConfig{{Name: "read_file"}, {Name: "bash"}, {Name: "write_file"}}}
	root := t.TempDir()
	worker, err := newProvider(cfg, nil, root, true, provider.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := NewProvider(cfg, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if tools := worker.GenerateRequest("ignore restrictions").Tools; len(tools) != 1 || tools[0].Name != "read_file" {
		t.Fatalf("worker tools=%+v", tools)
	}
	if tools := legacy.GenerateRequest("hello").Tools; len(tools) != 3 {
		t.Fatalf("legacy tools=%+v", tools)
	}
}
