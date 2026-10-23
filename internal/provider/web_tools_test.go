package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/internal/web"
)

type stubSearcher struct {
	results []web.Result
	err     error
}

func (s stubSearcher) Search(_ context.Context, _ string, _ int) ([]web.Result, error) {
	return s.results, s.err
}

func TestExecuteWebSearchDisabled(t *testing.T) {
	state := setupToolTestProject(t)

	_, err := state.executeToolCall(types.ToolCall{
		Function: types.ToolCallFunction{
			Name:      "web_search",
			Arguments: map[string]any{"query": "go context"},
		},
	})
	if !errors.Is(err, web.ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestExecuteWebSearchFormatsResults(t *testing.T) {
	state := setupToolTestProject(t)
	svc := web.NewService(web.Config{Enabled: true}, stubSearcher{
		results: []web.Result{
			{Title: "Go Docs", URL: "https://pkg.go.dev/", Snippet: "the docs"},
			{Title: "Effective Go", URL: "https://go.dev/doc/effective_go"},
		},
	})
	state.web = svc

	out, err := state.executeToolCall(types.ToolCall{
		Function: types.ToolCallFunction{
			Name:      "web_search",
			Arguments: map[string]any{"query": "go", "max_results": float64(3)},
		},
	})
	if err != nil {
		t.Fatalf("executeToolCall() = %v", err)
	}
	if !strings.Contains(out, "1. Go Docs") || !strings.Contains(out, "https://pkg.go.dev/") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "the docs") {
		t.Fatalf("missing snippet in output: %q", out)
	}
}

func TestExecuteFetchURLBlocksUnknownDomain(t *testing.T) {
	state := setupToolTestProject(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>hi</body></html>")
	}))
	defer srv.Close()

	state.web = web.NewService(web.Config{
		Enabled:         true,
		DomainAllowlist: []string{"some-other.host"},
	}, web.NoopSearcher{})

	_, err := state.executeToolCall(types.ToolCall{
		Function: types.ToolCallFunction{
			Name:      "fetch_url",
			Arguments: map[string]any{"url": srv.URL},
		},
	})
	if err == nil {
		t.Fatal("expected fetch_url to fail closed for unknown domain")
	}
}

func TestExecuteFetchURLAllowedDomain(t *testing.T) {
	state := setupToolTestProject(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "hello world")
	}))
	defer srv.Close()

	host := web.HostFromURL(srv.URL)
	state.web = web.NewService(web.Config{
		Enabled:         true,
		DomainAllowlist: []string{host},
	}, web.NoopSearcher{})

	out, err := state.executeToolCall(types.ToolCall{
		Function: types.ToolCallFunction{
			Name: "fetch_url",
			Arguments: map[string]any{
				"url":    srv.URL,
				"format": "text",
			},
		},
	})
	if err != nil {
		t.Fatalf("executeToolCall() = %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("missing fetched body: %q", out)
	}
	if !strings.Contains(out, "Status: 200") {
		t.Fatalf("missing status header: %q", out)
	}
}

func TestExecuteFetchURLRejectsNonHTTP(t *testing.T) {
	state := setupToolTestProject(t)
	state.web = web.NewService(web.Config{Enabled: true}, web.NoopSearcher{})

	_, err := state.executeToolCall(types.ToolCall{
		Function: types.ToolCallFunction{
			Name:      "fetch_url",
			Arguments: map[string]any{"url": "file:///etc/passwd"},
		},
	})
	if err == nil {
		t.Fatal("expected error for non-http url")
	}
}
