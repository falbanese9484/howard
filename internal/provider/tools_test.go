package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteWriteFileRejectsSymlinkEscape(t *testing.T) {
	root := setupToolTestProject(t)
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := executeWriteFile(map[string]any{
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
	root := setupToolTestProject(t)
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := executeMkdir(map[string]any{
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
	root := setupToolTestProject(t)

	_, err := executeWriteFile(map[string]any{
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
	root := setupToolTestProject(t)

	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	out, err := executeReadFile(map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("executeReadFile() = %v, want nil", err)
	}
	if out != "hello" {
		t.Fatalf("executeReadFile() = %q, want %q", out, "hello")
	}
}

func TestSetWorkspaceRootOverridesCWD(t *testing.T) {
	// The process CWD is one directory; the configured workspace root is another.
	setupToolTestProject(t)

	workspace := t.TempDir()
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resolvedWorkspace, "in.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatalf("write in.txt: %v", err)
	}

	SetWorkspaceRoot(resolvedWorkspace)
	t.Cleanup(func() { SetWorkspaceRoot("") })

	out, err := executeReadFile(map[string]any{"path": "in.txt"})
	if err != nil {
		t.Fatalf("executeReadFile() = %v, want nil", err)
	}
	if out != "inside" {
		t.Fatalf("executeReadFile() = %q, want %q", out, "inside")
	}

	// A path outside the configured root must still be rejected.
	outside := t.TempDir()
	if _, err := executeReadFile(map[string]any{"path": filepath.Join(outside, "x.txt")}); err == nil {
		t.Fatal("executeReadFile() succeeded for path outside workspace root, want error")
	}
}

func setupToolTestProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp project: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	// Ensure no leaked override from another test affects CWD-based resolution.
	SetWorkspaceRoot("")

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	return resolved
}
