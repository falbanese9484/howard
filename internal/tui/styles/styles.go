package styles

import "github.com/charmbracelet/lipgloss"

// Adaptive colors keep the chrome legible on both light and dark terminals.
var (
	Accent = lipgloss.AdaptiveColor{Light: "#087F8C", Dark: "#78DCCA"}
	Muted  = lipgloss.AdaptiveColor{Light: "#657482", Dark: "#8995A7"}
	Border = lipgloss.AdaptiveColor{Light: "#C4CDD5", Dark: "#384454"}
	Warm   = lipgloss.AdaptiveColor{Light: "#946200", Dark: "#E5BE78"}

	User           = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	AI             = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5264A4", Dark: "#ADBDF5"}).Bold(true)
	AITool         = lipgloss.NewStyle().Foreground(Warm)
	Error          = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B73848", Dark: "#F0929C"})
	MutedText      = lipgloss.NewStyle().Foreground(Muted)
	Connected      = MutedText
	ConnectedModel = lipgloss.NewStyle().Foreground(Accent)
	Logo           = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	Title          = lipgloss.NewStyle().Bold(true)
	Key            = lipgloss.NewStyle().Foreground(Accent)
	Rule           = lipgloss.NewStyle().Foreground(Border)
	InputBox       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent).Padding(0, 1)
	ThinkingTitle  = lipgloss.NewStyle().Foreground(Warm).Bold(true)
	ThinkingBox    = MutedText.Border(lipgloss.RoundedBorder()).BorderForeground(Border).Padding(0, 1)
	SelectorBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Border).Padding(1, 1)
)
