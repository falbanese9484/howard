//go:build !linux

package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/web"
)

func TestWorkerBashFailsClosedOnUnsupportedPlatform(t *testing.T) {
	state, err := NewWorkerExecutionState(t.TempDir(), web.Config{}, nil, ReadOnly, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.executeBash(context.Background(), map[string]any{"command": "echo forbidden > side-effect"})
	if err == nil || !strings.Contains(err.Error(), "requires Linux") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(state.root, "side-effect")); !os.IsNotExist(err) {
		t.Fatalf("unsupported worker launched shell: %v", err)
	}
}

func TestLegacyBashRemainsAvailableOnUnsupportedWorkerPlatform(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	state := setupToolTestProject(t)
	out, err := state.executeBash(context.Background(), map[string]any{"command": "printf legacy-compatible"})
	if err != nil || out != "legacy-compatible" {
		t.Fatalf("legacy bash = %q, %v", out, err)
	}
}
