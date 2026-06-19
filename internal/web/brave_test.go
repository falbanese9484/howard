package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBraveSearcherRequiresAPIKey(t *testing.T) {
	searcher := NewBraveSearcher("")

	_, err := searcher.Search(context.Background(), "go", 5)
	if err == nil || !strings.Contains(err.Error(), "BRAVE_SEARCH_API_KEY") {
		t.Fatalf("Search() error = %v, want missing key error", err)
	}
}

func TestBraveSearcherMapsWebResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			t.Errorf("X-Subscription-Token = %q, want test-key", r.Header.Get("X-Subscription-Token"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", r.Header.Get("Accept"))
		}
		q := r.URL.Query()
		if q.Get("q") != "go context" {
			t.Errorf("q = %q, want go context", q.Get("q"))
		}
		if q.Get("count") != "3" {
			t.Errorf("count = %q, want 3", q.Get("count"))
		}
		if q.Get("result_filter") != "web" {
			t.Errorf("result_filter = %q, want web", q.Get("result_filter"))
		}
		if q.Get("text_decorations") != "false" {
			t.Errorf("text_decorations = %q, want false", q.Get("text_decorations"))
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"web": {
				"results": [
					{"title":" Go Docs ","url":" https://pkg.go.dev/context ","description":" Context docs "},
					{"title":"No URL","description":"skip me"}
				]
			}
		}`)
	}))
	defer srv.Close()

	searcher := NewBraveSearcher("test-key")
	searcher.BaseURL = srv.URL
	searcher.Client = srv.Client()

	results, err := searcher.Search(context.Background(), "go context", 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, want 1: %#v", len(results), results)
	}
	if results[0] != (Result{Title: "Go Docs", URL: "https://pkg.go.dev/context", Snippet: "Context docs"}) {
		t.Fatalf("result = %#v", results[0])
	}
}

func TestBraveSearcherReturnsStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	searcher := NewBraveSearcher("test-key")
	searcher.BaseURL = srv.URL
	searcher.Client = srv.Client()

	_, err := searcher.Search(context.Background(), "go", 5)
	if err == nil || !strings.Contains(err.Error(), "status 429") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("Search() error = %v, want status/body error", err)
	}
}
