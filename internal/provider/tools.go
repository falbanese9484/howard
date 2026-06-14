package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/falbanese9484/agent-harness/internal/types"
)

// Sandbox utility functions for Agentic Tooling

const bashTimeout = 60 * time.Second

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
	case "bash":
		return executeBash(call.Function.Arguments)
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
	// We implement edit_file as a find-and-replace, to avoid the complexity of merging concurrent edits.
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
	// write_file creates a new file with the specified content. It fails if the file already exists.
	path := stringArg(args, "path", "")
	content := stringArg(args, "content", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := resolveNewProjectPath(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("file already exists: %s; use edit_file for existing files", path)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	parent := filepath.Dir(abs)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("created file %s", path), nil
}

func executeBash(args map[string]any) (string, error) {
	// TODO:Consider implementing a more structured command execution, rather than raw bash,
	// to improve security and reliability. i
	// For example, we could support a limited set of commands with structured arguments.
	command := stringArg(args, "command", "")
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	root, err := findProjectRoot()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), bashTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = root
	outputBytes, err := cmd.CombinedOutput()
	output := string(outputBytes)
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out after %s; output: %s", bashTimeout, output)
	}
	if err != nil {
		return output, fmt.Errorf("command failed: %w; output: %s", err, output)
	}
	if output == "" {
		output = "(no output)"
	}
	return output, nil
}

func executeMkdir(args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := resolveNewProjectPath(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return fmt.Sprintf("created directory %s", path), nil
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

func resolveNewProjectPath(path string) (string, error) {
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
	if !isProjectPath(root, abs) {
		return "", fmt.Errorf("path outside project root is not allowed: %s", path)
	}

	// For paths that do not exist yet, validate the nearest existing parent after
	// resolving symlinks so create operations cannot write through a project-local
	// symlink to an outside directory.
	existing := abs
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(existing)
		if parent == existing {
			return "", os.ErrNotExist
		}
		existing = parent
	}

	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	if !isProjectPath(root, resolvedExisting) {
		return "", fmt.Errorf("path outside project root is not allowed: %s", path)
	}

	rel, err := filepath.Rel(existing, abs)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedExisting, rel), nil
}

func isProjectPath(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.EvalSymlinks(dir)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
