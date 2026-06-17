package web

import "testing"

func TestSandboxAllowsExactAndWildcard(t *testing.T) {
	s := NewSandbox([]string{"pkg.go.dev", "*.github.com", "*.MOZILLA.org"})

	cases := []struct {
		host string
		want bool
	}{
		{"pkg.go.dev", true},
		{"PKG.GO.DEV", true},
		{"pkg.go.dev:443", true},
		{"api.github.com", true},
		{"raw.api.github.com", true},
		{"github.com", false}, // wildcard requires a leading dot match
		{"developer.mozilla.org", true},
		{"mozilla.org", false},
		{"example.com", false},
		{"", false},
	}

	for _, tc := range cases {
		got := s.Allows(tc.host)
		if got != tc.want {
			t.Errorf("Allows(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestSandboxIgnoresEmptyPatterns(t *testing.T) {
	s := NewSandbox([]string{"", "  ", "pkg.go.dev"})
	if !s.Allows("pkg.go.dev") {
		t.Fatal("expected pkg.go.dev to be allowed")
	}
	if s.Allows("anything.example") {
		t.Fatal("expected anything.example to be denied")
	}
}

func TestHostFromURL(t *testing.T) {
	cases := map[string]string{
		"https://pkg.go.dev/net/http":  "pkg.go.dev",
		"http://Example.COM:8080/path": "example.com",
		"not-a-url":                    "",
		"":                             "",
		"https://api.github.com/repos": "api.github.com",
	}
	for in, want := range cases {
		if got := HostFromURL(in); got != want {
			t.Errorf("HostFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
