package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/web"
)

func TestExecuteWriteFileRejectsSymlinkEscape(t *testing.T) {
	state := setupToolTestProject(t)
	root := state.root
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := state.executeWriteFile(map[string]any{
		"path":    filepath.Join("outside-link", "escaped.txt"),
		"content": "escaped",
	})
	if err == nil {
		t.Fatal("executeWriteFile() succeeded through symlink escape, want error")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("outside file exists after rejected write: %v", statErr)
	}
}

func TestExecuteMkdirRejectsSymlinkEscape(t *testing.T) {
	state := setupToolTestProject(t)
	root := state.root
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := state.executeMkdir(map[string]any{
		"path": filepath.Join("outside-link", "escaped-dir"),
	})
	if err == nil {
		t.Fatal("executeMkdir() succeeded through symlink escape, want error")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escaped-dir")); !os.IsNotExist(statErr) {
		t.Fatalf("outside directory exists after rejected mkdir: %v", statErr)
	}
}

func TestExecuteWriteFileAllowsNestedProjectPath(t *testing.T) {
	state := setupToolTestProject(t)
	root := state.root

	_, err := state.executeWriteFile(map[string]any{
		"path":    filepath.Join("nested", "file.txt"),
		"content": "ok",
	})
	if err != nil {
		t.Fatalf("executeWriteFile() = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(root, "nested", "file.txt")); err != nil {
		t.Fatalf("project file was not created: %v", err)
	}
}

func TestExecuteReadFileWorksWithoutGoMod(t *testing.T) {
	state := setupToolTestProject(t)
	root := state.root

	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	out, err := state.executeReadFile(map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("executeReadFile() = %v, want nil", err)
	}
	if out != "hello" {
		t.Fatalf("executeReadFile() = %q, want %q", out, "hello")
	}
}

func TestExecutionWorkspaceOverridesCWD(t *testing.T) {
	// The process CWD is one directory; the configured workspace root is another.
	t.Chdir(t.TempDir())

	workspace := t.TempDir()
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resolvedWorkspace, "in.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatalf("write in.txt: %v", err)
	}

	state, err := NewExecutionState(resolvedWorkspace, web.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Later process CWD changes must not redirect an existing execution.
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	out, err := state.executeReadFile(map[string]any{"path": "in.txt"})
	if err != nil {
		t.Fatalf("executeReadFile() = %v, want nil", err)
	}
	if out != "inside" {
		t.Fatalf("executeReadFile() = %q, want %q", out, "inside")
	}

	// A path outside the configured root must still be rejected.
	outside := t.TempDir()
	if _, err := state.executeReadFile(map[string]any{"path": filepath.Join(outside, "x.txt")}); err == nil {
		t.Fatal("executeReadFile() succeeded for path outside workspace root, want error")
	}
}

func setupToolTestProject(t *testing.T) *ExecutionState {
	t.Helper()
	state, err := NewExecutionState(t.TempDir(), web.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
