package components

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/falbanese9484/agent-harness/internal/tui/styles"
	"github.com/falbanese9484/agent-harness/internal/types"
)

type modelItem struct {
	name string
}

func (m modelItem) Title() string       { return m.name }
func (m modelItem) Description() string { return "" }
func (m modelItem) FilterValue() string { return m.name }

type ModelSelectedMsg struct{ Name string }
type ModelCancelMsg struct{}

type ModelSelector struct {
	List   list.Model
	Active bool
	width  int
	height int
}

var selectorKeys = struct {
	Select key.Binding
	Cancel key.Binding
}{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Cancel: key.NewBinding(key.WithKeys("esc", "q", "ctrl+c"), key.WithHelp("esc", "back")),
}

func NewModelSelector(width, height int) *ModelSelector {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(styles.Accent).BorderForeground(styles.Accent)
	l := list.New([]list.Item{}, delegate, width, height)
	l.Title = "Switch model"
	l.Styles.Title = styles.Logo
	l.Styles.HelpStyle = styles.MutedText.Padding(1, 0, 0, 2)
	l.DisableQuitKeybindings()
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{selectorKeys.Select, selectorKeys.Cancel}
	}
	l.SetShowHelp(true)

	return &ModelSelector{
		List:   l,
		width:  width,
		height: height,
	}
}

func (ms *ModelSelector) SetModels(models []types.Model) {
	items := make([]list.Item, len(models))
	for i, m := range models {
		items[i] = modelItem{name: m.Name}
	}
	ms.List.SetItems(items)
}

func (ms *ModelSelector) Update(msg tea.Msg) tea.Cmd {
	if !ms.Active {
		return nil
	}

	var cmd tea.Cmd
	ms.List, cmd = ms.List.Update(msg)

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, selectorKeys.Select):
			if item, ok := ms.List.SelectedItem().(modelItem); ok {
				ms.Active = false
				return func() tea.Msg { return ModelSelectedMsg{Name: item.name} }
			}
		case key.Matches(keyMsg, selectorKeys.Cancel):
			ms.Active = false
			return func() tea.Msg { return ModelCancelMsg{} }
		}
	}

	return cmd
}

func (ms *ModelSelector) View() string {
	if !ms.Active {
		return ""
	}
	return styles.SelectorBox.Width(max(1, ms.width-2)).Render(ms.List.View())
}

func (ms *ModelSelector) SetSize(width, height int) {
	ms.width = width
	ms.height = height
	ms.List.SetShowStatusBar(height >= 14)
	ms.List.SetShowPagination(height >= 14)
	ms.List.SetShowHelp(height >= 10)
	ms.List.SetSize(max(1, width-4), max(1, height-4))
}

func (ms *ModelSelector) Toggle() {
	ms.Active = !ms.Active
	if ms.Active {
		ms.List.ResetSelected()
	}
}
