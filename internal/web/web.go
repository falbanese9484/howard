// Package web provides web search and URL fetching for the agent harness.
//
// The package is intentionally small: a Searcher interface, a Fetch helper,
// and a domain-allowlist sandbox. Search backends and confirmation UX are
// pluggable via the Service struct so the harness can swap in real
// implementations without changing tool dispatch.
package web

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Result is a single search result.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Searcher returns ranked results for a query. Implementations should respect
// max as a soft cap and may return fewer results.
type Searcher interface {
	Search(ctx context.Context, query string, max int) ([]Result, error)
}

// ConfirmFunc is invoked when a request targets a domain not on the
// allowlist. It should return true to allow the request and false to deny.
// Returning an error denies the request and surfaces the error to the caller.
//
// A nil ConfirmFunc means "fail closed": unknown domains are always denied.
type ConfirmFunc func(ctx context.Context, host, reason string) (bool, error)

// Config controls the active Service. Zero values are safe; a disabled
// service rejects all calls with a clear error.
type Config struct {
	Enabled         bool
	SearchBackend   string
	DomainAllowlist []string
	MaxFetchBytes   int64
	FetchTimeout    int // seconds
}

// Service is the single entry point used by tool dispatch. It holds the
// active sandbox, search backend, and confirmation callback.
type Service struct {
	mu       sync.RWMutex
	cfg      Config
	sandbox  *Sandbox
	searcher Searcher
	confirm  ConfirmFunc
}

// NewService constructs a Service from cfg. The returned Service has no
// confirmation callback; callers should attach one with SetConfirm if they
// want to allow interactive overrides for unknown domains.
func NewService(cfg Config, searcher Searcher) *Service {
	cfg.DomainAllowlist = append([]string(nil), cfg.DomainAllowlist...)
	return &Service{
		cfg:      cfg,
		sandbox:  NewSandbox(cfg.DomainAllowlist),
		searcher: searcher,
	}
}

// SetConfirm registers (or clears) the confirmation callback.
func (s *Service) SetConfirm(fn ConfirmFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confirm = fn
}

// Enabled reports whether web tools are currently enabled.
func (s *Service) Enabled() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Enabled
}

// Search runs the configured search backend. Search results carry URLs but
// do not perform any fetching themselves.
func (s *Service) Search(ctx context.Context, query string, max int) ([]Result, error) {
	if s == nil || !s.Enabled() {
		return nil, ErrDisabled
	}
	s.mu.RLock()
	searcher := s.searcher
	s.mu.RUnlock()
	if searcher == nil {
		return nil, errors.New("no search backend configured; set web.search_backend in config and run `howard auth search <backend>`")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is required")
	}
	if max <= 0 {
		max = 5
	}
	if max > 10 {
		max = 10
	}
	return searcher.Search(ctx, query, max)
}

// AuthorizeHost checks the sandbox and, if needed, invokes the confirmation
// callback. A non-nil error means the request must not proceed.
func (s *Service) AuthorizeHost(ctx context.Context, host, reason string) error {
	if s == nil || !s.Enabled() {
		return ErrDisabled
	}
	s.mu.RLock()
	sandbox := s.sandbox
	confirm := s.confirm
	s.mu.RUnlock()

	if sandbox.Allows(host) {
		return nil
	}
	if confirm == nil {
		return fmt.Errorf("host %q is not on the web allowlist; add it to web.domain_allowlist or run howard in interactive mode", host)
	}
	allowed, err := confirm(ctx, host, reason)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("user denied access to %q", host)
	}
	return nil
}

// Config returns a snapshot of the current configuration.
func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	cfg.DomainAllowlist = append([]string(nil), cfg.DomainAllowlist...)
	return cfg
}

// ErrDisabled is returned when web tools are invoked while disabled.
var ErrDisabled = errors.New("web tools are disabled; set web.enabled=true in config")
