package app

import (
	"os"
	"path/filepath"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/provider"
	"github.com/falbanese9484/agent-harness/internal/types"
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

	switch cfg.Provider {
	case "ollama":
		return provider.NewOllama(log, cfg, cache), nil
	case "openrouter":
		return provider.NewOpenRouter(log, cfg, cache)
	default:
		return nil, &UnsupportedProviderError{Provider: cfg.Provider}
	}
}

type UnsupportedProviderError struct {
	Provider string
}

func (e *UnsupportedProviderError) Error() string {
	return "unsupported provider: " + e.Provider
}
