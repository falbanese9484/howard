package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/falbanese9484/agent-harness/internal/types"
)

type sessionProvider struct {
	types.Provider
	resets int
	err    error
}

func (p *sessionProvider) ResetConversation() error {
	p.resets++
	return p.err
}

func TestNewCommandResetsSession(t *testing.T) {
	p := &sessionProvider{}
	model := New(Config{Width: 80, Height: 24, ModelName: "selected-model", Provider: p})
	model.chatView.Append("old conversation")
	model.clipboard.Push("old prompt")
	model.currentResp = "old response"
	model.currentThinking = "old thinking"
	model.err = errors.New("old error")
	model.input.Textarea.SetValue("/new")

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || p.resets != 1 {
		t.Fatalf("/new returned command %v, resets = %d", cmd != nil, p.resets)
	}
	if len(model.chatView.Messages) != 0 || model.currentResp != "" || model.currentThinking != "" || model.err != nil || model.isResponding {
		t.Fatal("/new retained conversation state")
	}
	if model.input.Textarea.Value() != "" || model.clipboard.Prev() != "" || model.clipboard.Next() != "" {
		t.Fatal("/new retained prompt or prompt history")
	}
	if model.modelName != "selected-model" || model.provider != p {
		t.Fatal("/new changed provider or selected model")
	}
	if !strings.Contains(ansi.Strip(model.View()), "Start with a prompt.") {
		t.Fatal("/new did not restore welcome screen")
	}
	// Resetting an already empty session is also valid.
	model.input.Textarea.SetValue("/new")
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.resets != 2 {
		t.Fatal("could not reset an empty session")
	}
}

func TestSlashCommandsDoNotSubmitPrompts(t *testing.T) {
	for _, test := range []struct {
		name       string
		command    string
		responding bool
		resetErr   error
		wantResets int
		wantErr    bool
	}{
		{name: "busy", command: "/new", responding: true},
		{name: "reset failure", command: "/new", resetErr: errors.New("busy"), wantResets: 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := &sessionProvider{err: test.resetErr}
			model := New(Config{Width: 80, Height: 24, Provider: p})
			model.chatView.Append("existing conversation")
			model.clipboard.Push("existing prompt")
			model.isResponding = test.responding
			model.input.Textarea.SetValue(test.command)
			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd != nil || p.resets != test.wantResets || (model.err != nil) != test.wantErr {
				t.Fatalf("command = %v, resets = %d, error = %v", cmd != nil, p.resets, model.err)
			}
			if len(model.chatView.Messages) != 1 || model.clipboard.Prev() != "existing prompt" || strings.TrimSpace(model.input.Textarea.Value()) != test.command {
				t.Fatal("rejected command changed session or input")
			}
		})
	}
}

func TestUpdateKeepsTypedRunesOutOfHistoryNavigation(t *testing.T) {
	model := New(Config{Width: 80, Height: 24})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(*Model)

	if got := model.input.Textarea.Value(); got != "a" {
		t.Fatalf("textarea value = %q, want %q", got, "a")
	}
}

func TestWelcomeFitsViewport(t *testing.T) {
	model := New(Config{Width: 80, Height: 24})
	for _, size := range []struct{ width, height int }{
		{76, 16}, {28, 12}, {28, 6}, {28, 3}, {12, 3}, {5, 1},
	} {
		model.chatView.Viewport.Width = size.width
		model.chatView.Viewport.Height = size.height
		welcome := model.welcome()
		if width, height := lipgloss.Size(welcome); width > size.width || height > size.height {
			t.Errorf("welcome at %dx%d is %dx%d", size.width, size.height, width, height)
		}
		plain := ansi.Strip(welcome)
		if size.height >= 12 {
			if !strings.Contains(plain, "A little help.") || !strings.Contains(plain, "Explore a project,") {
				t.Errorf("full welcome missing at %dx%d: %q", size.width, size.height, plain)
			}
		} else if size.width >= 28 && !strings.Contains(plain, "Start with a prompt.") {
			t.Errorf("compact welcome missing at %dx%d: %q", size.width, size.height, plain)
		}
	}
}

func TestLayoutFitsTerminal(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{
		{Width: 32, Height: 12},
		{Width: 80, Height: 24},
		{Width: 160, Height: 48},
	} {
		model := New(Config{Width: 100, Height: 30, ModelName: "local/model"})
		model.handleResize(size)
		for _, state := range []string{"welcome", "thinking", "selector"} {
			switch state {
			case "thinking":
				model.currentThinking = "Considering the layout and how it adapts to this terminal."
				model.updateLayout()
			case "selector":
				model.mode = modeModelSelect
				model.selector.SetModels([]types.Model{{Name: "local/model"}, {Name: "a-provider/a-model-with-a-long-name-for-small-terminals"}})
				model.selector.Toggle()
			}
			view := model.View()
			if width, height := lipgloss.Size(view); width > size.Width || height > size.Height {
				t.Errorf("%s at %dx%d: view is %dx%d\n%s", state, size.Width, size.Height, width, height, view)
			}
		}
	}
}
