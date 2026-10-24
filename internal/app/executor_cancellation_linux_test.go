//go:build linux

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/executor"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func TestExecutorShellCancellationTerminalFollowsProcessCleanup(t *testing.T) {
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	var requests atomic.Int32
	http.DefaultClient = &http.Client{Transport: runTransport(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		msg := types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{Function: types.ToolCallFunction{
			Name: "bash", Arguments: map[string]any{"command": "echo $$ > parent.pid; sleep 300 & echo $! > child.pid; wait"},
		}}, {Function: types.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"path": "later", "content": "forbidden"}}}}}
		data, err := json.Marshal(map[string]any{"message": msg, "done": true})
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data) + "\n"))}, err
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	req := domain.RunRequest{RunID: "shell", SessionID: "session", Provider: "ollama", Model: "test", Prompt: "hello", WorkspaceRoot: root}
	var events []domain.RunEvent
	finished := make(chan error, 1)
	go func() {
		finished <- NewExecutor(&config.AgentConfig{Provider: "ollama"}, nil).Execute(ctx, req, executor.Observer{Lifecycle: func(event domain.RunEvent) {
			if event.Status.Terminal() {
				parentBytes, pErr := os.ReadFile(filepath.Join(root, "parent.pid"))
				childBytes, cErr := os.ReadFile(filepath.Join(root, "child.pid"))
				parent, _ := strconv.Atoi(strings.TrimSpace(string(parentBytes)))
				child, _ := strconv.Atoi(strings.TrimSpace(string(childBytes)))
				if pErr != nil || cErr != nil || parent <= 0 || child <= 0 {
					t.Error("missing process identities before terminal")
				} else {
					if err := syscall.Kill(parent, 0); err != syscall.ESRCH {
						t.Errorf("shell not reaped before terminal: %v", err)
					}
					stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child))
					if err == nil {
						fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
						if len(fields) == 0 || (fields[0] != "Z" && fields[0] != "X") {
							t.Errorf("child executing at terminal: %s", stat)
						}
					} else if !os.IsNotExist(err) {
						t.Error(err)
					}
				}
			}
			events = append(events, event)
		}})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(root, "child.pid"))
		if err == nil && strings.TrimSpace(string(data)) != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shell child did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-finished:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shell cancellation did not stop")
	}
	checkLifecycle(t, req, events, domain.RunQueued, domain.RunRunning, domain.RunCanceled)
	if requests.Load() != 1 {
		t.Fatalf("started another model round: %d", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(root, "later")); !os.IsNotExist(err) {
		t.Fatalf("later tool ran: %v", err)
	}
}
