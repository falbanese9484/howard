package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/falbanese9484/agent-harness/internal/types"
)

func TestUpdateKeepsTypedRunesOutOfHistoryNavigation(t *testing.T) {
	model := New(Config{Width: 80, Height: 24})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(*Model)

	if got := model.input.Textarea.Value(); got != "a" {
		t.Fatalf("textarea value = %q, want %q", got, "a")
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
