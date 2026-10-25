package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func typeCommandText(m *Model, text string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func TestCommandMenuFiltersWhileTyping(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	for _, text := range []string{"/", "n", "e", "w"} {
		typeCommandText(m, text)
		if matches := m.commandMatches(); len(matches) != 1 || matches[0].name != "/new" {
			t.Fatalf("matches for %q = %#v", m.input.Textarea.Value(), matches)
		}
		if view := ansi.Strip(m.View()); !strings.Contains(view, "› /new  Start a new session") || !strings.Contains(view, "dismiss") {
			t.Fatalf("command menu or keyboard help missing: %q", view)
		}
	}
	typeCommandText(m, "x")
	if len(m.commandMatches()) != 0 {
		t.Fatal("menu retained nonmatching commands")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.commandMatches()) != 1 {
		t.Fatal("menu did not reappear after deleting nonmatching text")
	}
	for _, prompt := range []string{"normal prompt", "look at /new", "/new extra", "/new\n"} {
		m.input.Textarea.SetValue(prompt)
		if len(m.commandMatches()) != 0 {
			t.Errorf("unexpected menu for %q", prompt)
		}
	}
}

func TestCommandMenuNavigationAndCompletion(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	m.commands = append(m.commands, slashCommand{name: "/next"}, slashCommand{name: "/help"})
	m.clipboard.Push("previous prompt")
	m.chatView.Append(strings.Repeat("old chat\n", 50))
	m.chatView.Viewport.GotoTop()
	typeCommandText(m, "/")
	for _, step := range []struct {
		key  tea.KeyType
		want int
	}{
		{tea.KeyDown, 1}, {tea.KeyDown, 2}, {tea.KeyDown, 0}, {tea.KeyUp, 2},
	} {
		m.Update(tea.KeyMsg{Type: step.key})
		if m.commandIndex != step.want || m.input.Textarea.Value() != "/" || m.clipboard.cursor != 1 || m.chatView.Viewport.YOffset != 0 {
			t.Fatalf("navigation changed input, history, or viewport; selection = %d, want %d", m.commandIndex, step.want)
		}
	}
	// Typing narrows the list and resets the selection.
	typeCommandText(m, "n")
	if m.commandIndex != 0 || len(m.commandMatches()) != 2 {
		t.Fatal("typing did not filter and reset selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.input.Textarea.Value() != "/next" {
		t.Fatalf("completed input = %q, want /next", m.input.Textarea.Value())
	}
	// Completion leaves the cursor at the end so typing can continue.
	typeCommandText(m, " extra")
	if m.input.Textarea.Value() != "/next extra" {
		t.Fatalf("typing after completion = %q", m.input.Textarea.Value())
	}
}

func TestCommandMenuEnterRunsSelection(t *testing.T) {
	p := &sessionProvider{}
	m := New(Config{Width: 80, Height: 24, Provider: p})
	m.commands = append([]slashCommand{{name: "/help"}}, m.commands...)
	m.chatView.Append("old chat")
	typeCommandText(m, "/")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.resets != 1 || m.err != nil || m.input.Textarea.Value() != "" || len(m.chatView.Messages) != 0 || len(m.commandMatches()) != 0 {
		t.Fatal("enter did not execute the highlighted /new command")
	}
}

func TestCommandMenuDismissAndBusy(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	typeCommandText(m, "/")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil || len(m.commandMatches()) != 0 || m.input.Textarea.Value() != "/" {
		t.Fatal("escape did not dismiss menu while preserving input")
	}
	typeCommandText(m, "n")
	if len(m.commandMatches()) != 1 {
		t.Fatal("typing after dismissal did not reopen menu")
	}
	m.isResponding = true
	if len(m.commandMatches()) != 0 {
		t.Fatal("menu available while response is in progress")
	}
	m.isResponding = false
	m.input.Textarea.Reset()
	m.clipboard.Push("previous prompt")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Textarea.Value() != "previous prompt" {
		t.Fatal("history navigation unavailable outside command menu")
	}
}

func TestCommandMenuFitsTerminal(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 32, Height: 12}, {Width: 80, Height: 24}, {Width: 160, Height: 48}} {
		m := New(Config{Width: size.Width, Height: size.Height})
		for range 20 {
			m.commands = append(m.commands, slashCommand{name: "/long-command", description: "A long command description for narrow terminals"})
		}
		typeCommandText(m, "/")
		for range len(m.commands) - 1 {
			m.Update(tea.KeyMsg{Type: tea.KeyDown})
		}
		if !strings.Contains(ansi.Strip(m.renderCommandMenu()), "› /long-command") {
			t.Fatal("selected command not visible after scrolling menu")
		}
		if width, height := lipgloss.Size(m.View()); width > size.Width || height > size.Height {
			t.Errorf("menu at %dx%d is %dx%d", size.Width, size.Height, width, height)
		}
	}
}
