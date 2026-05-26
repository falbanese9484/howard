package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/falbanese9484/agent-harness/internal/types"
)

func executeToolCall(call types.ToolCall) (string, error) {
	switch call.Function.Name {
	case "ls":
		return executeLS(call.Function.Arguments)
	case "read_file":
		return executeReadFile(call.Function.Arguments)
	case "edit_file":
		return executeEditFile(call.Function.Arguments)
	case "write_file":
		return executeWriteFile(call.Function.Arguments)
	case "mkdir":
		return executeMkdir(call.Function.Arguments)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Function.Name)
	}
}

func executeLS(args map[string]any) (string, error) {
	path := stringArg(args, "path", ".")
	abs, err := resolveProjectPath(path, true)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var b strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(&b, "%s\t%s\t%d bytes\n", kind, entry.Name(), info.Size())
	}
	return b.String(), nil
}

func executeReadFile(args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := resolveProjectPath(path, true)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", path)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func executeEditFile(args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	oldText := stringArg(args, "old_text", "")
	newText := stringArg(args, "new_text", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if oldText == "" {
		return "", fmt.Errorf("old_text is required")
	}

	abs, err := resolveProjectPath(path, true)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", fmt.Errorf("old_text not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_text is not unique in %s; found %d matches", path, count)
	}

	updated := strings.Replace(content, oldText, newText, 1)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(updated), info.Mode().Perm()); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s", path), nil
}

func executeWriteFile(args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	content := stringArg(args, "content", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := resolveProjectPath(path, false)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("file already exists: %s; use edit_file for existing files", path)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	parent := filepath.Dir(abs)
	if err := ensureProjectParent(parent); err != nil {
		return "", err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("created file %s", path), nil
}

func executeMkdir(args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := resolveProjectPath(path, false)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return fmt.Sprintf("created directory %s", path), nil
}

func ensureProjectParent(parent string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	if parent != root && !strings.HasPrefix(parent, root+string(os.PathSeparator)) {
		return fmt.Errorf("parent path outside project root is not allowed")
	}
	return nil
}

func stringArg(args map[string]any, name, fallback string) string {
	if raw, ok := args[name].(string); ok {
		return raw
	}
	return fallback
}

func resolveProjectPath(path string, mustExist bool) (string, error) {
	root, err := findProjectRoot()
	if err != nil {
		return "", err
	}

	requested := path
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}
	abs, err := filepath.Abs(requested)
	if err != nil {
		return "", err
	}

	if mustExist {
		abs, err = filepath.EvalSymlinks(abs)
		if err != nil {
			return "", err
		}
	}

	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path outside project root is not allowed: %s", path)
	}
	return abs, nil
}
