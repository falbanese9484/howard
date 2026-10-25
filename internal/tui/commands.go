package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/falbanese9484/agent-harness/internal/tui/styles"
)

type slashCommand struct {
	name        string
	description string
}

func (m *Model) commandMatches() []slashCommand {
	if m.mode != modeChat || m.isResponding || m.commandsHidden {
		return nil
	}
	prefix := strings.TrimLeft(m.input.Textarea.Value(), " \t")
	if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, " \t\r\n") {
		return nil
	}
	var matches []slashCommand
	for _, command := range m.commands {
		if strings.HasPrefix(command.name, prefix) {
			matches = append(matches, command)
		}
	}
	return matches
}

// Consume menu keys before the textarea, viewport, or prompt history sees them.
func (m *Model) handleCommandMenuKey(key tea.KeyMsg) bool {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return false
	}
	m.commandIndex = min(m.commandIndex, len(matches)-1)
	switch key.Type {
	case tea.KeyUp:
		m.commandIndex = (m.commandIndex + len(matches) - 1) % len(matches)
	case tea.KeyDown:
		m.commandIndex = (m.commandIndex + 1) % len(matches)
	case tea.KeyTab:
		m.input.Textarea.SetValue(matches[m.commandIndex].name)
		m.input.Textarea.CursorEnd()
	case tea.KeyEnter:
		m.handleSlashCommand(matches[m.commandIndex].name)
	case tea.KeyEscape:
		m.commandsHidden = true
	default:
		return false
	}
	return true
}

func (m *Model) renderCommandMenu() string {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return ""
	}
	selected := min(m.commandIndex, len(matches)-1)
	limit := max(1, m.height/4)
	start := max(0, selected-limit+1)
	end := min(len(matches), start+limit)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		marker := "  "
		style := styles.MutedText
		if i == selected {
			marker = "› "
			style = styles.User
		}
		line := marker + matches[i].name + "  " + matches[i].description
		lines = append(lines, style.Render(ansi.Truncate(line, m.chatView.Viewport.Width, "…")))
	}
	return strings.Join(lines, "\n")
}
