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

func setupToolTestProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

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

	return root
}
