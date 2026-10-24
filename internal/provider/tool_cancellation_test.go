package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/web"
)

func TestCanceledDispatcherStartsNoTool(t *testing.T) {
	state := setupToolTestProject(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, name := range []string{"ls", "read_file", "edit_file", "write_file", "mkdir", "bash", "web_search", "fetch_url"} {
		_, err := state.executeToolCall(ctx, toolCall(name, map[string]any{"path": "unexpected", "content": "changed", "old_text": "old", "new_text": "new", "command": "touch unexpected", "query": "test", "url": "https://example.com"}))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s error = %v, want canceled", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(state.root, "unexpected")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled dispatch caused side effect: %v", err)
	}
}

func TestCancellationPreservesCompletedFileSideEffect(t *testing.T) {
	state := setupToolTestProject(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := state.executeToolCall(ctx, toolCall("write_file", map[string]any{"path": "completed", "content": "keep"})); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := state.executeToolCall(ctx, toolCall("edit_file", map[string]any{"path": "completed", "old_text": "keep", "new_text": "changed"})); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(state.root, "completed"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("completed side effect = %q, %v", data, err)
	}
}

type cancelSearcher struct {
	entered chan struct{}
	stopped chan struct{}
}

func (s cancelSearcher) Search(ctx context.Context, _ string, _ int) ([]web.Result, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.stopped)
	return nil, ctx.Err()
}

func TestWebSearchUsesRunContext(t *testing.T) {
	state := setupToolTestProject(t)
	searcher := cancelSearcher{make(chan struct{}), make(chan struct{})}
	state.web = web.NewService(web.Config{Enabled: true}, searcher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := state.executeWebSearch(ctx, map[string]any{"query": "blocked"})
		finished <- err
	}()
	select {
	case <-searcher.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("search did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		select {
		case <-searcher.stopped:
		default:
			t.Fatal("search returned before execution stopped")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("search did not stop")
	}
}

func TestFetchURLUsesRunContext(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(stopped)
	}))
	defer srv.Close()
	state := setupToolTestProject(t)
	state.web = web.NewService(web.Config{Enabled: true, DomainAllowlist: []string{web.HostFromURL(srv.URL)}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := state.executeFetchURL(ctx, map[string]any{"url": srv.URL})
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not stop")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request was not canceled")
	}
}
