package components

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/falbanese9484/agent-harness/internal/tui/styles"
)

type InputArea struct {
	Textarea textarea.Model
}

func NewInputArea() *InputArea {
	ta := textarea.New()
	ta.Placeholder = "What would you like to build?"
	ta.Focus()
	ta.Prompt = "› "
	ta.SetHeight(3)
	ta.KeyMap.InsertNewline.SetEnabled(false)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = styles.User
	ta.FocusedStyle.Placeholder = styles.MutedText
	ta.ShowLineNumbers = false

	return &InputArea{Textarea: ta}
}
