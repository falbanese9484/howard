package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/falbanese9484/agent-harness/pkg/config"
)

func TestAuthOpenRouterStoresKeyAndSwitchesProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	err := Run([]string{"auth", "openrouter", "-key", "test-key"}, IO{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("Run(auth openrouter) error = %v", err)
	}

	cfg, err := config.LoadUserAgentConfig()
	if err != nil {
		t.Fatalf("LoadUserAgentConfig() error = %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Fatalf("Provider = %q, want openrouter", cfg.Provider)
	}
	if cfg.ApiKey != "" {
		t.Fatalf("config ApiKey = %q, want empty", cfg.ApiKey)
	}

	auth, err := config.LoadAuthConfig()
	if err != nil {
		t.Fatalf("LoadAuthConfig() error = %v", err)
	}
	if auth.Credentials["openrouter"].ApiKey != "test-key" {
		t.Fatalf("openrouter auth key = %q, want test-key", auth.Credentials["openrouter"].ApiKey)
	}

	runtimeCfg, err := config.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}
	if runtimeCfg.ApiKey != "test-key" {
		t.Fatalf("runtime ApiKey = %q, want test-key", runtimeCfg.ApiKey)
	}
	if !strings.Contains(stdout.String(), "Authenticated openrouter") {
		t.Fatalf("stdout = %q, want auth confirmation", stdout.String())
	}
}

func TestAuthOllamaSwitchesProviderWithoutConfigKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.SaveAgentConfig(&config.AgentConfig{
		Provider: "openrouter",
	}); err != nil {
		t.Fatalf("SaveAgentConfig() error = %v", err)
	}
	if err := config.SetProviderAuth("openrouter", "test-key"); err != nil {
		t.Fatalf("SetProviderAuth() error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := Run([]string{"auth", "ollama"}, IO{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("Run(auth ollama) error = %v", err)
	}

	cfg, err := config.LoadUserAgentConfig()
	if err != nil {
		t.Fatalf("LoadUserAgentConfig() error = %v", err)
	}
	if cfg.Provider != "ollama" {
		t.Fatalf("Provider = %q, want ollama", cfg.Provider)
	}
	if cfg.ApiKey != "" {
		t.Fatalf("ApiKey = %q, want empty", cfg.ApiKey)
	}
	runtimeCfg, err := config.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}
	if runtimeCfg.ApiKey != "" {
		t.Fatalf("runtime ApiKey = %q, want empty", runtimeCfg.ApiKey)
	}
	if !strings.Contains(stdout.String(), "Authenticated ollama") {
		t.Fatalf("stdout = %q, want auth confirmation", stdout.String())
	}
}

func TestAuthRequiresProvider(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run([]string{"auth"}, IO{Stdout: &stdout, Stderr: &stderr})
	if err == nil || err.Error() != "provider is required" {
		t.Fatalf("Run(auth) error = %v, want provider is required", err)
	}
}

func TestAuthRejectsUnsupportedProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	err := Run([]string{"auth", "anthropic", "-key", "test-key"}, IO{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err == nil || err.Error() != "unsupported provider: anthropic" {
		t.Fatalf("Run(auth anthropic) error = %v, want unsupported provider", err)
	}
}
