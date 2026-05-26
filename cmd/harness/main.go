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
)

func main() {
	model := "qwen3.5:9b"
	if len(os.Args) > 1 {
		model = os.Args[1]
	}

	p := tea.NewProgram(initModel(model), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func initModel(modelName string) tea.Model {
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
	prov, err := provider.NewOpenRouter(log, "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", "qwen/qwen3.7-max", cache)
	if err != nil {
		panic(err)
	}

	bus := chat.NewBus(log, prov)
	responses := make(chan *types.ChatResponse, 100)

	go bus.Start(responses)

	m := tui.New(tui.Config{
		Width:     width,
		Height:    height,
		ModelName: modelName,
		Provider:  prov,
		Bus:       bus,
		Responses: responses,
		Logger:    log,
	})

	// Set initial content
	logo := styles.Logo.Render(tui.Logo) + styles.Title.Render(tui.Tagline) + "\n" +
		styles.Connected.Render("Connected to: ") + styles.ConnectedModel.Render(modelName) + "\n\n"
	m.SetInitialContent(logo)

	return m
}
