package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceSearchRespectsEnabled(t *testing.T) {
	svc := NewService(Config{Enabled: false}, NoopSearcher{})
	if _, err := svc.Search(context.Background(), "go", 5); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Search disabled: err = %v, want ErrDisabled", err)
	}
}

func TestServiceAuthorizeHostFailsClosed(t *testing.T) {
	svc := NewService(Config{
		Enabled:         true,
		DomainAllowlist: []string{"pkg.go.dev"},
	}, NoopSearcher{})

	if err := svc.AuthorizeHost(context.Background(), "pkg.go.dev", "test"); err != nil {
		t.Fatalf("expected pkg.go.dev to be allowed: %v", err)
	}
	err := svc.AuthorizeHost(context.Background(), "evil.example", "test")
	if err == nil {
		t.Fatal("expected unknown host to be denied when no confirm callback is set")
	}
	if !strings.Contains(err.Error(), "evil.example") {
		t.Fatalf("error should mention denied host, got %v", err)
	}
}

func TestServiceAuthorizeHostUsesCallback(t *testing.T) {
	svc := NewService(Config{Enabled: true}, NoopSearcher{})

	called := false
	svc.SetConfirm(func(ctx context.Context, host, reason string) (bool, error) {
		called = true
		if host != "example.com" {
			t.Errorf("callback host = %q, want example.com", host)
		}
		return true, nil
	})

	if err := svc.AuthorizeHost(context.Background(), "example.com", "fetch"); err != nil {
		t.Fatalf("AuthorizeHost() = %v, want nil", err)
	}
	if !called {
		t.Fatal("confirm callback was not invoked")
	}

	svc.SetConfirm(func(ctx context.Context, host, reason string) (bool, error) {
		return false, nil
	})
	if err := svc.AuthorizeHost(context.Background(), "example.com", "fetch"); err == nil {
		t.Fatal("expected denial when callback returns false")
	}
}

func TestFetchEnforcesMaxBytes(t *testing.T) {
	body := strings.Repeat("a", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	res, err := Fetch(context.Background(), srv.URL, FetchOptions{
		Format:   "text",
		MaxBytes: 100,
	})
	if err != nil {
		t.Fatalf("Fetch() = %v", err)
	}
	if !strings.Contains(res.Body, "[truncated to 100 bytes]") {
		t.Fatalf("expected truncation marker, got body=%q", res.Body)
	}
}

func TestFetchRejectsBinaryContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0x00, 0x01, 0x02})
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), srv.URL, FetchOptions{Format: "text"})
	if err == nil {
		t.Fatal("expected error for binary content type")
	}
	if !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchConvertsHTMLToMarkdown(t *testing.T) {
	html := `<html><head><title>x</title></head><body>
		<h1>Hello</h1>
		<p>This is a <a href="https://example.com">link</a>.</p>
		<script>var bad = 1;</script>
		<ul><li>one</li><li>two</li></ul>
	</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	}))
	defer srv.Close()

	res, err := Fetch(context.Background(), srv.URL, FetchOptions{Format: "markdown"})
	if err != nil {
		t.Fatalf("Fetch() = %v", err)
	}
	if !strings.Contains(res.Body, "# Hello") {
		t.Errorf("missing h1 markdown: %q", res.Body)
	}
	if !strings.Contains(res.Body, "(https://example.com)") {
		t.Errorf("missing link reference: %q", res.Body)
	}
	if strings.Contains(res.Body, "var bad") {
		t.Errorf("script body leaked into output: %q", res.Body)
	}
	if !strings.Contains(res.Body, "- one") || !strings.Contains(res.Body, "- two") {
		t.Errorf("missing list items: %q", res.Body)
	}
}
