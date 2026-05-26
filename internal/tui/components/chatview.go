package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

type ChatView struct {
	Viewport viewport.Model
	Messages []string
}

func NewChatView(width, height int) *ChatView {
	return &ChatView{
		Viewport: viewport.New(width, height),
		Messages: []string{},
	}
}

func (c *ChatView) Render() {
	content := lipgloss.NewStyle().
		Width(c.Viewport.Width).
		Render(strings.Join(c.Messages, "\n"))
	c.Viewport.SetContent(content)
	c.Viewport.GotoBottom()
}

func (c *ChatView) Append(msg string) {
	c.Messages = append(c.Messages, msg)
	c.Render()
}
