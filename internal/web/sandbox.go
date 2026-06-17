package web

import (
	"net/url"
	"strings"
)

// Sandbox matches hosts against a list of patterns. Patterns may be exact
// hostnames ("pkg.go.dev") or wildcard suffixes ("*.github.com"). Matching
// is case-insensitive and ignores port suffixes.
type Sandbox struct {
	exact     map[string]struct{}
	wildcards []string
}

// NewSandbox builds a Sandbox from a list of patterns.
func NewSandbox(patterns []string) *Sandbox {
	s := &Sandbox{exact: map[string]struct{}{}}
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "*.") {
			suffix := strings.TrimPrefix(p, "*")
			s.wildcards = append(s.wildcards, suffix)
			continue
		}
		s.exact[p] = struct{}{}
	}
	return s
}

// Allows reports whether host is permitted by the sandbox. An empty host is
// never permitted.
func (s *Sandbox) Allows(host string) bool {
	if s == nil {
		return false
	}
	host = normalizeHost(host)
	if host == "" {
		return false
	}
	if _, ok := s.exact[host]; ok {
		return true
	}
	for _, suffix := range s.wildcards {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// HostFromURL extracts the lowercase hostname from a URL string. Returns an
// empty string if the URL cannot be parsed or has no host.
func HostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normalizeHost(u.Host)
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(host, ":"); i != -1 {
		// Strip a trailing port if it doesn't look like an IPv6 address.
		if !strings.Contains(host, "]") {
			host = host[:i]
		}
	}
	return host
}
