package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/falbanese9484/agent-harness/pkg/config"
)

func TestResolveWorkspaceRootPrecedence(t *testing.T) {
	overrideDir := mustResolved(t, t.TempDir())
	configDir := mustResolved(t, t.TempDir())
	cwdDir := mustResolved(t, t.TempDir())

	chdir(t, cwdDir)

	t.Run("override wins", func(t *testing.T) {
		got, err := resolveWorkspaceRoot(&config.AgentConfig{WorkspaceRoot: configDir}, overrideDir)
		if err != nil {
			t.Fatalf("resolveWorkspaceRoot() = %v", err)
		}
		if got != overrideDir {
			t.Fatalf("got %q, want override %q", got, overrideDir)
		}
	})

	t.Run("config used when no override", func(t *testing.T) {
		got, err := resolveWorkspaceRoot(&config.AgentConfig{WorkspaceRoot: configDir}, "")
		if err != nil {
			t.Fatalf("resolveWorkspaceRoot() = %v", err)
		}
		if got != configDir {
			t.Fatalf("got %q, want config %q", got, configDir)
		}
	})

	t.Run("cwd fallback", func(t *testing.T) {
		got, err := resolveWorkspaceRoot(&config.AgentConfig{}, "")
		if err != nil {
			t.Fatalf("resolveWorkspaceRoot() = %v", err)
		}
		if got != cwdDir {
			t.Fatalf("got %q, want cwd %q", got, cwdDir)
		}
	})
}

func TestResolveWorkspaceRootErrors(t *testing.T) {
	dir := t.TempDir()

	t.Run("nonexistent path", func(t *testing.T) {
		if _, err := resolveWorkspaceRoot(&config.AgentConfig{}, filepath.Join(dir, "missing")); err == nil {
			t.Fatal("resolveWorkspaceRoot() = nil error for missing path, want error")
		}
	})

	t.Run("not a directory", func(t *testing.T) {
		file := filepath.Join(dir, "file.txt")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if _, err := resolveWorkspaceRoot(&config.AgentConfig{}, file); err == nil {
			t.Fatal("resolveWorkspaceRoot() = nil error for file path, want error")
		}
	})
}

func mustResolved(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %q: %v", dir, err)
	}
	return resolved
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}
