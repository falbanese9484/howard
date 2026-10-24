package app

import (
	"github.com/falbanese9484/agent-harness/internal/domain"
	"github.com/falbanese9484/agent-harness/internal/executor"
	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

// NewExecutor snapshots configuration and constructs a fresh provider, tool
// state, and conversation per run. Requests select the model and workspace, but
// must match the configured provider so credentials cannot cross backends.
// SessionID is correlation only; this executor does not retain conversation history.
func NewExecutor(cfg *config.AgentConfig, log *logger.Logger) *executor.Executor {
	if cfg == nil {
		return executor.New(nil)
	}
	base := *cfg
	base.Tools = append([]config.ToolConfig(nil), cfg.Tools...)
	for i := range base.Tools {
		base.Tools[i].Parameters = append([]config.ParameterConfig(nil), cfg.Tools[i].Parameters...)
	}
	base.Web.DomainAllowlist = append([]string(nil), cfg.Web.DomainAllowlist...)
	base.SystemPrompt.Sections = append([]config.SystemPromptSection(nil), cfg.SystemPrompt.Sections...)
	for i := range base.SystemPrompt.Sections {
		base.SystemPrompt.Sections[i].Content = append([]string(nil), cfg.SystemPrompt.Sections[i].Content...)
	}
	return executor.New(func(request domain.RunRequest) (executor.Turn, error) {
		if request.Provider != base.Provider {
			return nil, executor.ErrSetup
		}
		runConfig := base
		runConfig.Model = request.Model
		p, err := newProvider(&runConfig, log, request.WorkspaceRoot, true)
		if err != nil {
			return nil, err
		}
		return executor.NewProviderTurn(p), nil
	})
}
