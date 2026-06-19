package app

import (
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

func NewProvider(cfg *config.AgentConfig, log *logger.Logger) (types.Provider, error) {
	cache := types.NewModelCache(3600)

	provider.SetWebService(buildWebService(cfg))

	switch cfg.Provider {
	case "ollama":
		return provider.NewOllama(log, cfg, cache), nil
	case "openrouter":
		return provider.NewOpenRouter(log, cfg, cache)
	default:
		return nil, &UnsupportedProviderError{Provider: cfg.Provider}
	}
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
