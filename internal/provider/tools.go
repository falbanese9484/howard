package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/internal/web"
)

// Sandbox utility functions for Agentic Tooling

const bashTimeout = 60 * time.Second

// ExecutionState owns a worker's fixed workspace and web service. Construct one
// per provider/conversation; file checks are not an OS or shell sandbox.
type ExecutionState struct {
	root   string
	web    *web.Service
	worker bool
}

// NewExecutionState resolves the workspace once, independently of later CWD
// changes. The searcher must be safe for concurrent use if explicitly shared.
func NewExecutionState(root string, cfg web.Config, searcher web.Searcher) (*ExecutionState, error) {
	if root == "" {
		return nil, fmt.Errorf("workspace root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory: %s", abs)
	}
	return &ExecutionState{root: abs, web: web.NewService(cfg, searcher)}, nil
}

// NewWorkerExecutionState requires verified process-group cleanup for bash.
// Unlike legacy CLI/TUI state, workers fail closed on unsupported platforms.
func NewWorkerExecutionState(root string, cfg web.Config, searcher web.Searcher) (*ExecutionState, error) {
	state, err := NewExecutionState(root, cfg, searcher)
	if err != nil {
		return nil, err
	}
	state.worker = true
	return state, nil
}

func (s *ExecutionState) executeToolCall(ctx context.Context, call types.ToolCall) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := s.findProjectRoot(); err != nil {
		return "", err
	}
	switch call.Function.Name {
	case "ls":
		return s.executeLS(call.Function.Arguments)
	case "read_file":
		return s.executeReadFile(call.Function.Arguments)
	case "edit_file":
		return s.executeEditFile(call.Function.Arguments)
	case "write_file":
		return s.executeWriteFile(call.Function.Arguments)
	case "mkdir":
		return s.executeMkdir(call.Function.Arguments)
	case "bash":
		return s.executeBash(ctx, call.Function.Arguments)
	case "web_search":
		return s.executeWebSearch(ctx, call.Function.Arguments)
	case "fetch_url":
		return s.executeFetchURL(ctx, call.Function.Arguments)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Function.Name)
	}
}

func (s *ExecutionState) executeLS(args map[string]any) (string, error) {
	path := stringArg(args, "path", ".")
	abs, err := s.resolveProjectPath(path, true)
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

func (s *ExecutionState) executeReadFile(args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := s.resolveProjectPath(path, true)
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

func (s *ExecutionState) executeEditFile(args map[string]any) (string, error) {
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

	abs, err := s.resolveProjectPath(path, true)
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

func (s *ExecutionState) executeWriteFile(args map[string]any) (string, error) {
	// write_file creates a new file with the specified content. It fails if the file already exists.
	path := stringArg(args, "path", "")
	content := stringArg(args, "content", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := s.resolveNewProjectPath(path)
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

func (s *ExecutionState) executeBash(ctx context.Context, args map[string]any) (string, error) {
	// TODO:Consider implementing a more structured command execution, rather than raw bash,
	// to improve security and reliability. i
	// For example, we could support a limited set of commands with structured arguments.
	command := stringArg(args, "command", "")
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	root, err := s.findProjectRoot()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()

	outputBytes, err := runBash(ctx, root, command, s.worker)
	output := string(outputBytes)
	if ctx.Err() == context.Canceled {
		return output, context.Canceled
	}
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out: %w; output: %s", ctx.Err(), output)
	}
	if err != nil {
		return output, fmt.Errorf("command failed: %w; output: %s", err, output)
	}
	if output == "" {
		output = "(no output)"
	}
	return output, nil
}

func (s *ExecutionState) executeMkdir(args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	abs, err := s.resolveNewProjectPath(path)
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

func (s *ExecutionState) resolveProjectPath(path string, mustExist bool) (string, error) {
	root, err := s.findProjectRoot()
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

	if !isProjectPath(root, abs) {
		return "", fmt.Errorf("path outside project root is not allowed: %s", path)
	}
	return abs, nil
}

func (s *ExecutionState) resolveNewProjectPath(path string) (string, error) {
	root, err := s.findProjectRoot()
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
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func (s *ExecutionState) findProjectRoot() (string, error) {
	if s == nil || s.root == "" {
		return "", fmt.Errorf("execution state is required")
	}
	return s.root, nil
}
