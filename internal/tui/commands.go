package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/falbanese9484/agent-harness/internal/tui/styles"
)

type slashCommand struct {
	name        string
	description string
	execute     func(*Model) tea.Cmd
}

// Only an unpadded, single-token prefix opens the command menu.
func commandPrefix(input string) (string, bool) {
	return input, strings.HasPrefix(input, "/") && strings.IndexFunc(input, unicode.IsSpace) < 0
}

func (m *Model) commandMatches() []slashCommand {
	if m.mode != modeChat || m.isResponding || m.commandsHidden {
		return nil
	}
	prefix, valid := commandPrefix(m.input.Textarea.Value())
	if !valid {
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

func (m *Model) handleSlashCommand(input string) (bool, tea.Cmd) {
	if m.isResponding {
		return false, nil
	}
	for _, command := range m.commands {
		if command.name == input {
			return true, command.execute(m)
		}
	}
	return false, nil
}

func (m *Model) startNewSession() tea.Cmd {
	if err := m.provider.ResetConversation(); err != nil {
		m.err = err
		return nil
	}
	m.chatView.Messages = nil
	m.currentResp = ""
	m.currentThinking = ""
	m.err = nil
	m.clipboard = historyInit(m.clipboard.limit)
	m.commandIndex = 0
	m.commandsHidden = false
	m.input.Textarea.Reset()
	m.updateLayout()
	m.chatView.Viewport.GotoTop()
	return nil
}

// Consume menu keys before the textarea, viewport, or prompt history sees them.
func (m *Model) handleCommandMenuKey(key tea.KeyMsg) (bool, tea.Cmd) {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return false, nil
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
		return m.handleSlashCommand(matches[m.commandIndex].name)
	case tea.KeyEscape:
		m.commandsHidden = true
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) renderCommandMenu() string {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return ""
	}
	selected := min(m.commandIndex, len(matches)-1)
	available := m.height - 4 - lipgloss.Height(m.renderInput())
	if thinking := m.renderThinkingBox(); thinking != "" {
		available -= lipgloss.Height(thinking)
	}
	limit := min(max(1, m.height/4), available)
	if limit < 1 {
		return ""
	}
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
