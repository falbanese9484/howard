package components

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	Select: key.NewBinding(key.WithKeys("enter")),
	Cancel: key.NewBinding(key.WithKeys("esc", "q")),
}

func NewModelSelector(width, height int) *ModelSelector {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), width, height)
	l.Title = "Select a Model"
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
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(ms.width)

	return style.Render(ms.List.View())
}

func (ms *ModelSelector) Toggle() {
	ms.Active = !ms.Active
	if ms.Active {
		ms.List.ResetSelected()
	}
}
