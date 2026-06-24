package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/provider"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/internal/web"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func NewLogger() (*logger.Logger, error) {
	logPath := os.Getenv("LOG_FILE_PATH")
	if logPath == "" {
		logPath = "./logs/"
	}

	return logger.New(logger.Config{
		FilePath: filepath.Join(logPath, "harness.log"),
		FileOnly: true,
	})
}

func NewProvider(cfg *config.AgentConfig, log *logger.Logger, workspaceOverride string) (types.Provider, error) {
	cache := types.NewModelCache(3600)

	root, err := resolveWorkspaceRoot(cfg, workspaceOverride)
	if err != nil {
		return nil, err
	}
	provider.SetWorkspaceRoot(root)

	provider.SetWebService(buildWebService(cfg))

	switch cfg.Provider {
	case "ollama":
		return provider.NewOllama(log, cfg, cache), nil
	case "openrouter":
		return provider.NewOpenRouter(log, cfg, cache)
	case "bedrock":
		return provider.NewBedrock(log, cfg, cache)
	default:
		return nil, &UnsupportedProviderError{Provider: cfg.Provider}
	}
}

// resolveWorkspaceRoot determines the sandbox root for file tools. Precedence:
// the CLI override, then config.WorkspaceRoot, then the current working
// directory. The chosen path is expanded, made absolute, verified to be a
// directory, and symlink-resolved so the provider's containment checks remain
// valid.
func resolveWorkspaceRoot(cfg *config.AgentConfig, override string) (string, error) {
	candidate := strings.TrimSpace(override)
	if candidate == "" && cfg != nil {
		candidate = strings.TrimSpace(cfg.WorkspaceRoot)
	}
	if candidate == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine working directory: %w", err)
		}
		candidate = cwd
	} else if strings.HasPrefix(candidate, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand home directory: %w", err)
		}
		candidate = filepath.Join(home, strings.TrimPrefix(candidate, "~"))
	}

	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root %q: %w", candidate, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("workspace root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", abs)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root symlinks %q: %w", abs, err)
	}
	return resolved, nil
}

// buildWebService translates config.WebConfig into a runtime web.Service.
// When web access is disabled the returned service still answers Enabled()
// as false, which causes the web tools to refuse with a clear error.
func buildWebService(cfg *config.AgentConfig) *web.Service {
	wc := cfg.Web
	searcher := web.Searcher(web.NoopSearcher{})
	switch strings.ToLower(strings.TrimSpace(wc.SearchBackend)) {
	case "", "noop":
		searcher = web.NoopSearcher{}
	case "brave":
		apiKey := strings.TrimSpace(wc.ApiKey)
		if apiKey == "" {
			apiKey = os.Getenv("BRAVE_SEARCH_API_KEY")
		}
		searcher = web.NewBraveSearcher(apiKey)
	}

	svc := web.NewService(web.Config{
		Enabled:         wc.Enabled,
		SearchBackend:   wc.SearchBackend,
		DomainAllowlist: wc.DomainAllowlist,
		MaxFetchBytes:   wc.MaxFetchBytes,
		FetchTimeout:    wc.FetchTimeoutSeconds,
	}, searcher)
	return svc
}

type UnsupportedProviderError struct {
	Provider string
}

func (e *UnsupportedProviderError) Error() string {
	return "unsupported provider: " + e.Provider
}
