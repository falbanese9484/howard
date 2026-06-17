package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/falbanese9484/agent-harness/internal/web"
)

const (
	defaultFetchTimeout = 30 * time.Second
	defaultMaxFetch     = int64(2 * 1024 * 1024)
	webToolTimeout      = 45 * time.Second
)

func executeWebSearch(args map[string]any) (string, error) {
	svc := currentWebService()
	if svc == nil || !svc.Enabled() {
		return "", web.ErrDisabled
	}

	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	max := intArg(args, "max_results", 5)

	ctx, cancel := context.WithTimeout(context.Background(), webToolTimeout)
	defer cancel()

	results, err := svc.Search(ctx, query, max)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "no results", nil
	}

	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if snippet := strings.TrimSpace(r.Snippet); snippet != "" {
			fmt.Fprintf(&b, "   %s\n", snippet)
		}
	}
	return b.String(), nil
}

func executeFetchURL(args map[string]any) (string, error) {
	svc := currentWebService()
	if svc == nil || !svc.Enabled() {
		return "", web.ErrDisabled
	}

	url := strings.TrimSpace(stringArg(args, "url", ""))
	if url == "" {
		return "", fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("url must start with http:// or https://")
	}

	format := strings.ToLower(strings.TrimSpace(stringArg(args, "format", "markdown")))
	if format != "markdown" && format != "text" {
		return "", fmt.Errorf("format must be 'markdown' or 'text'")
	}

	host := web.HostFromURL(url)
	if host == "" {
		return "", fmt.Errorf("could not parse host from url")
	}

	cfg := svc.Config()
	timeout := defaultFetchTimeout
	if cfg.FetchTimeout > 0 {
		timeout = time.Duration(cfg.FetchTimeout) * time.Second
	}
	maxBytes := defaultMaxFetch
	if cfg.MaxFetchBytes > 0 {
		maxBytes = cfg.MaxFetchBytes
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()

	if err := svc.AuthorizeHost(ctx, host, "fetch_url: "+url); err != nil {
		return "", err
	}

	result, err := web.Fetch(ctx, url, web.FetchOptions{
		Format:   format,
		MaxBytes: maxBytes,
		Timeout:  timeout,
	})
	if err != nil {
		return "", err
	}

	header := fmt.Sprintf("URL: %s\nStatus: %d\nContent-Type: %s\nBytes: %d\n\n",
		result.URL, result.Status, result.ContentType, result.Bytes)
	return header + result.Body, nil
}

func intArg(args map[string]any, name string, fallback int) int {
	if raw, ok := args[name]; ok {
		switch v := raw.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		case string:
			if v == "" {
				return fallback
			}
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
	}
	return fallback
}
