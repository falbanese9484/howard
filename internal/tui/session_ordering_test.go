package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/falbanese9484/agent-harness/internal/chat"
	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

type orderedSessionProvider struct {
	sessionProvider
	failOld bool
}

func (p *orderedSessionProvider) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{Prompt: prompt}
}

func (p *orderedSessionProvider) Chat(conn *types.BusConnector) {
	conn.ResponseChan <- &types.ChatResponse{Thinking: conn.Request.Prompt + " thinking"}
	conn.ResponseChan <- &types.ChatResponse{Response: conn.Request.Prompt + " response", Kind: types.ResponseKindAssistant}
	if p.failOld && conn.Request.Prompt == "old" {
		conn.ErrorChan <- errors.New("old failure")
		return
	}
	conn.DoneChannel <- true
}

func TestNewSessionDoesNotReceivePreviousTurnContent(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "completed"
		if failure {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			log, err := logger.New(logger.Config{FileOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			p := &orderedSessionProvider{failOld: failure}
			bus := chat.NewBus(log, p)
			responses := make(chan *types.ChatResponse, 100) // Match the real TUI wiring.
			go bus.Start(responses)
			m := New(Config{Width: 80, Height: 24, ModelName: "selected-model", Provider: p, Bus: bus, Responses: responses, Logger: log})
			submitAndFinishTurn(t, m, "old")
			if !strings.Contains(strings.Join(m.chatView.Messages, "\n"), "old response") {
				t.Fatal("terminal overtook old response")
			}
			m.input.Textarea.SetValue("/new")
			m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if p.resets != 1 || len(m.chatView.Messages) != 0 || m.clipboard.Prev() != "" || m.modelName != "selected-model" {
				t.Fatal("reset did not clear history and preserve the model")
			}
			submitAndFinishTurn(t, m, "fresh")
			view := ansi.Strip(m.View())
			if strings.Contains(view, "old") || !strings.Contains(view, "fresh response") || m.currentThinking != "" || m.currentResp != "" {
				t.Fatalf("fresh session contains stale or incomplete output: %q", view)
			}
			if len(responses) != 0 {
				t.Fatal("content remained queued after terminal")
			}
		})
	}
}

func submitAndFinishTurn(t *testing.T, m *Model, prompt string) {
	t.Helper()
	m.input.Textarea.SetValue(prompt)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for m.isResponding {
		if cmd == nil {
			t.Fatal("turn stopped waiting before completion")
		}
		messages := make(chan tea.Msg, 1)
		go func(next tea.Cmd) { messages <- next() }(cmd)
		select {
		case msg := <-messages:
			_, cmd = m.Update(msg)
		case <-time.After(time.Second):
			t.Fatal("turn did not complete")
		}
	}
}
