package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUpdateKeepsTypedRunesOutOfHistoryNavigation(t *testing.T) {
	model := New(Config{Width: 80, Height: 24})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(*Model)

	if got := model.input.Textarea.Value(); got != "a" {
		t.Fatalf("textarea value = %q, want %q", got, "a")
	}
}
