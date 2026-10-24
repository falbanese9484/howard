package tuiapp

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/falbanese9484/agent-harness/internal/app"
	"github.com/falbanese9484/agent-harness/internal/chat"
	"github.com/falbanese9484/agent-harness/internal/tui"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func Run() error {
	cfg, err := config.LoadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	model, err := initModel(cfg)
	if err != nil {
		return err
	}
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func initModel(cfg *config.AgentConfig) (tea.Model, error) {
	width, height, _ := term.GetSize(0)

	log, err := app.NewLogger()
	if err != nil {
		return nil, fmt.Errorf("initialize logger: %w", err)
	}

	prov, err := app.NewProvider(cfg, log, "")
	if err != nil {
		return nil, fmt.Errorf("initialize provider: %w", err)
	}

	bus := chat.NewBus(log, prov)
	responses := make(chan *types.ChatResponse, 100)

	go bus.Start(responses)

	m := tui.New(tui.Config{
		Width:     width,
		Height:    height,
		ModelName: cfg.Model,
		Provider:  prov,
		Bus:       bus,
		Responses: responses,
		Logger:    log,
	})

	return m, nil
}
