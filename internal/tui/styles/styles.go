package styles

import "github.com/charmbracelet/lipgloss"

var (
	User = lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	AI = lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	AITool = lipgloss.NewStyle().
		Foreground(lipgloss.Color("220")).
		Bold(true)

	Connected = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("241"))

	ConnectedModel = Connected.
			Foreground(lipgloss.Color("75"))

	Logo = lipgloss.NewStyle().
		Foreground(lipgloss.Color("75")).
		Bold(true)

	Title = lipgloss.NewStyle().
		Bold(true).
		Padding(2)

	ThinkingTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("228")).
			Bold(true)

	ThinkingBox = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)
)
