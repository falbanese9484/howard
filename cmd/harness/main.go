package main

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/falbanese9484/agent-harness/internal/chat"
	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/provider"
	"github.com/falbanese9484/agent-harness/internal/tui"
	"github.com/falbanese9484/agent-harness/internal/tui/styles"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

func main() {
	cfg, err := config.LoadAgentConfig()
	if err != nil {
		log.Fatal("Failed to load config: ", err)
	}

	p := tea.NewProgram(initModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func initModel(cfg *config.AgentConfig) tea.Model {
	width, height, _ := term.GetSize(0)

	logPath := os.Getenv("LOG_FILE_PATH")
	if logPath == "" {
		logPath = "./logs/"
	}

	log, err := logger.New(logger.Config{
		FilePath: logPath + "harness.log",
		FileOnly: true,
	})
	if err != nil {
		panic(err)
	}

	cache := types.NewModelCache(3600)
	var prov types.Provider

	switch cfg.Provider {
	case "ollama":
		prov = provider.NewOllama(log, cfg, cache)
	case "openrouter":
		prov, err = provider.NewOpenRouter(log, cfg, cache)
		if err != nil {
			log.Fatal("Failed to initialize provider: ", err)
		}
	default:
		log.Fatal("Unsupported provider: ", cfg.Provider)
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

	// Set initial content
	logo := styles.Logo.Render(tui.Logo) + styles.Title.Render(tui.Tagline) + "\n" +
		styles.Connected.Render("Connected to: ") + styles.ConnectedModel.Render(cfg.Model) + "\n\n"
	m.SetInitialContent(logo)

	return m
}
