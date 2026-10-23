package tuiapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/app"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func TestInitModelReturnsProviderError(t *testing.T) {
	t.Setenv("LOG_FILE_PATH", t.TempDir())
	model, err := initModel(&config.AgentConfig{Provider: "bedrock", WorkspaceRoot: t.TempDir()})
	var unsupported *app.UnsupportedProviderError
	if !errors.As(err, &unsupported) || unsupported.Provider != "bedrock" {
		t.Fatalf("initModel error = %v, want unsupported bedrock provider", err)
	}
	if model != nil {
		t.Fatal("initialization failure returned a model")
	}
}

func TestInitModelReturnsLoggerError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOG_FILE_PATH", file)
	model, err := initModel(&config.AgentConfig{Provider: "ollama"})
	if err == nil || !strings.Contains(err.Error(), "initialize logger:") {
		t.Fatalf("initModel error = %v, want logger initialization error", err)
	}
	if model != nil {
		t.Fatal("initialization failure returned a model")
	}
}
