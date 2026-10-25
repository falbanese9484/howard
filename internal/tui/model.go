package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/falbanese9484/agent-harness/internal/chat"
	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/tui/components"
	"github.com/falbanese9484/agent-harness/internal/tui/styles"
	"github.com/falbanese9484/agent-harness/internal/types"
)

type mode int

const (
	modeChat mode = iota
	modeModelSelect

	thinkingContentHeight = 5
)

type (
	responseMsg *types.ChatResponse
	errMsg      error
)

type Model struct {
	input           *components.InputArea
	chatView        *components.ChatView
	selector        *components.ModelSelector
	bus             *chat.Bus
	provider        types.Provider
	responses       chan *types.ChatResponse
	renderer        *glamour.TermRenderer
	logger          *logger.Logger
	modelName       string
	currentResp     string
	currentThinking string
	isResponding    bool
	width           int
	height          int
	mode            mode
	err             error
	clipboard       *history
	commands        []slashCommand
	commandIndex    int
	commandsHidden  bool
}

type Config struct {
	Width     int
	Height    int
	ModelName string
	Provider  types.Provider
	Bus       *chat.Bus
	Responses chan *types.ChatResponse
	Logger    *logger.Logger
}

func New(cfg Config) *Model {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)

	mainWidth := cfg.Width * 2 / 3

	m := &Model{
		input:     components.NewInputArea(),
		chatView:  components.NewChatView(mainWidth, cfg.Height/2),
		selector:  components.NewModelSelector(mainWidth, cfg.Height/4),
		bus:       cfg.Bus,
		provider:  cfg.Provider,
		responses: cfg.Responses,
		renderer:  renderer,
		logger:    cfg.Logger,
		modelName: cfg.ModelName,
		width:     cfg.Width,
		height:    cfg.Height,
		mode:      modeChat,
		clipboard: historyInit(10),
		commands: []slashCommand{
			{name: "/new", description: "Start a new session", execute: (*Model).startNewSession},
		},
	}
	m.updateLayout()
	return m
}

func (m *Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mode == modeModelSelect {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, m.selector.Update(msg)
		}
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		if handled, cmd := m.handleCommandMenuKey(key); handled {
			m.updateLayout()
			return m, cmd
		}
	}
	previousInput := m.input.Textarea.Value()
	var inputCmd, viewCmd tea.Cmd
	m.input.Textarea, inputCmd = m.input.Textarea.Update(msg)
	m.chatView.Viewport, viewCmd = m.chatView.Viewport.Update(msg)
	if m.input.Textarea.Value() != previousInput {
		m.commandIndex = 0
		m.commandsHidden = false
		m.updateLayout()
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case responseMsg:
		return m.handleResponse(msg)
	case tea.KeyMsg:
		model, cmd := m.handleKey(msg)
		m.updateLayout()
		return model, cmd
	case errMsg:
		m.err = msg
		return m, nil
	case components.ModelSelectedMsg:
		m.modelName = msg.Name
		m.provider.SetModel(msg.Name)
		m.chatView.Append(formatMsg("System", "Switched to: "+msg.Name, styles.AI))
		m.mode = modeChat
		return m, nil
	case components.ModelCancelMsg:
		m.mode = modeChat
		return m, nil
	}

	return m, tea.Batch(inputCmd, viewCmd)
}

func (m *Model) View() string {
	if m.mode == modeModelSelect {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.selector.View())
	}

	width := m.chatView.Viewport.Width
	header := styles.Logo.Render("◆ howard") + styles.MutedText.Render("  /  ") + styles.ConnectedModel.Render(m.modelName)
	sections := []string{
		ansi.Truncate(header, width, "…"),
		styles.Rule.Render(strings.Repeat("─", width)),
		m.chatView.Viewport.View(),
	}
	if thinking := m.renderThinkingBox(); thinking != "" {
		sections = append(sections, thinking)
	}
	if commands := m.renderCommandMenu(); commands != "" {
		sections = append(sections, commands)
	}
	sections = append(sections, m.renderInput(), m.renderFooter())
	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content)
}

func (m *Model) renderInput() string {
	return styles.InputBox.Width(max(1, m.chatView.Viewport.Width-2)).Render(m.input.Textarea.View())
}

func (m *Model) renderFooter() string {
	status := styles.ConnectedModel.Render("● ready")
	if m.isResponding {
		status = styles.AITool.Render("● working")
	}
	if m.err != nil {
		status = styles.Error.Render("● " + m.err.Error())
	}
	help := styles.Key.Render("enter") + styles.MutedText.Render(" send  ") +
		styles.Key.Render("/") + styles.MutedText.Render(" commands  ") +
		styles.Key.Render("ctrl+f") + styles.MutedText.Render(" models  ") +
		styles.Key.Render("↑/↓") + styles.MutedText.Render(" history  ") +
		styles.Key.Render("esc") + styles.MutedText.Render(" quit")
	if len(m.commandMatches()) > 0 {
		help = styles.Key.Render("↑/↓") + styles.MutedText.Render(" select  ") +
			styles.Key.Render("enter") + styles.MutedText.Render(" run  ") +
			styles.Key.Render("tab") + styles.MutedText.Render(" complete  ") +
			styles.Key.Render("esc") + styles.MutedText.Render(" dismiss")
	}
	width := m.chatView.Viewport.Width
	gap := width - lipgloss.Width(status) - lipgloss.Width(help)
	if gap < 2 {
		return ansi.Truncate(status+"  "+help, width, "…")
	}
	return status + strings.Repeat(" ", gap) + help
}

func (m *Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.updateLayout()
	if len(m.chatView.Messages) > 0 {
		m.chatView.Render()
	}
	return m, nil
}

func (m *Model) renderCurrentResponse() string {
	rendered, err := m.renderer.Render(m.currentResp)
	if err != nil {
		return m.currentResp
	}
	return rendered
}

func (m *Model) commitCurrentResponse() {
	if strings.TrimSpace(m.currentResp) == "" {
		m.currentResp = ""
		return
	}

	m.chatView.Append(formatMsg(m.modelName, m.renderCurrentResponse(), styles.AI))
	m.currentResp = ""
}

func (m *Model) handleResponse(msg responseMsg) (tea.Model, tea.Cmd) {
	if msg == nil {
		return m, nil
	}

	if msg.Kind == types.ResponseKindError {
		m.commitCurrentResponse()
		m.chatView.Append(formatMsg("Error", strings.TrimSpace(msg.Response), styles.Error))
		m.currentThinking = ""
		m.isResponding = false
		m.updateLayout()
		return m, nil
	}

	if msg.Kind == types.ResponseKindToolStatus {
		m.commitCurrentResponse()
		m.chatView.Append(formatMsg("Tool", strings.TrimSpace(msg.Response), styles.AITool))
		if !msg.Done {
			return m, waitForResponse(m.responses)
		}
		m.currentThinking = ""
		m.isResponding = false
		m.updateLayout()
		return m, nil
	}

	if msg.Thinking != "" {
		m.currentThinking += msg.Thinking
		m.updateLayout()
	}
	if msg.Response != "" {
		m.currentResp += msg.Response
		all := append(m.chatView.Messages, formatMsg(m.modelName, m.renderCurrentResponse(), styles.AI))
		m.chatView.Viewport.SetContent(
			lipgloss.NewStyle().Width(m.chatView.Viewport.Width).Render(strings.Join(all, "\n\n")),
		)
		m.chatView.Viewport.GotoBottom()
	}
	if !msg.Done {
		return m, waitForResponse(m.responses)
	}

	m.commitCurrentResponse()
	m.currentThinking = ""
	m.isResponding = false
	m.updateLayout()
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp, tea.KeyDown:
		if m.mode == modeModelSelect {
			return m, m.selector.Update(msg)
		}
		// Navigate clipboard

		if msg.Type == tea.KeyUp {
			prompt := m.clipboard.Prev()
			if prompt != "" {
				m.input.Textarea.SetValue(prompt)
			}
		} else {
			m.input.Textarea.SetValue(m.clipboard.Next())
		}
		return m, nil

	case tea.KeyCtrlC, tea.KeyEscape:
		if m.mode == modeModelSelect {
			m.selector.Toggle()
			m.mode = modeChat
			return m, nil
		}
		return m, tea.Quit
	case tea.KeyEnter:
		if m.isResponding {
			return m, nil
		}

		prompt := m.input.Textarea.Value()
		if strings.TrimSpace(prompt) == "" {
			return m, nil
		}
		if prompt == "exit" {
			return m, tea.Quit
		}
		if handled, cmd := m.handleSlashCommand(prompt); handled {
			return m, cmd
		}

		m.clipboard.Push(prompt)

		m.err = nil
		m.currentResp = ""
		m.currentThinking = ""
		m.isResponding = true
		m.updateLayout()
		m.chatView.Append(formatMsg("You", prompt, styles.User))
		m.input.Textarea.Reset()
		req := m.provider.GenerateRequest(prompt)
		go m.bus.Send(req)
		return m, waitForResponse(m.responses)
	case tea.KeyCtrlF:
		if m.isResponding {
			m.logger.Info("user attempted to switch model while response is in progress, ignoring")
			return m, nil
		}
		m.mode = modeModelSelect
		models, err := m.provider.RetrieveModels()
		if err != nil {
			m.logger.Error("failed to load models", "error", err)
		}
		m.selector.SetModels(models)
		m.selector.Toggle()
	}
	return m, nil
}

func (m *Model) updateLayout() {
	mainWidth := max(5, min(100, m.width-4))
	widthChanged := m.chatView.Viewport.Width != mainWidth
	m.chatView.Viewport.Width = mainWidth
	m.input.Textarea.SetWidth(mainWidth - 4)
	m.input.Textarea.Placeholder = ansi.Truncate("What would you like to build?", max(1, mainWidth-7), "…")
	m.input.Textarea.SetHeight(3)
	if m.height < 18 {
		m.input.Textarea.SetHeight(1)
	}
	m.selector.SetSize(mainWidth, max(1, m.height-4))

	reserved := 3 + lipgloss.Height(m.renderInput())
	if thinking := m.renderThinkingBox(); thinking != "" {
		reserved += lipgloss.Height(thinking)
	}
	if commands := m.renderCommandMenu(); commands != "" {
		reserved += lipgloss.Height(commands)
	}
	m.chatView.Viewport.Height = max(1, m.height-reserved)
	if len(m.chatView.Messages) == 0 {
		m.chatView.Viewport.SetContent(m.welcome())
	}
	if widthChanged {
		if renderer, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(max(1, mainWidth-4))); err == nil {
			m.renderer = renderer
		}
	}
}

func (m *Model) welcome() string {
	width := m.chatView.Viewport.Width
	logo := styles.Logo.Render(strings.TrimSpace(Logo))
	if width < 40 || m.chatView.Viewport.Height < 12 {
		logo = styles.Logo.Render("◆ howard")
	}
	text := logo + "\n\n" + styles.Title.Render("A little help. A lot of possibility.") + "\n\n" +
		styles.MutedText.Render("Explore a project, work through an idea,\nor build something new. Start with a prompt.")
	style := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	rendered := style.Render(text)
	if lipgloss.Height(rendered) > m.chatView.Viewport.Height {
		rendered = style.Render(styles.Logo.Render("◆ howard") + "\n\n" + styles.MutedText.Render("Start with a prompt."))
	}
	if lipgloss.Height(rendered) > m.chatView.Viewport.Height {
		rendered = style.Render(styles.MutedText.Render(ansi.Truncate("Start with a prompt.", width, "…")))
	}
	return lipgloss.Place(width, m.chatView.Viewport.Height, lipgloss.Center, lipgloss.Center,
		rendered)
}

func (m *Model) renderThinkingBox() string {
	if strings.TrimSpace(m.currentThinking) == "" {
		return ""
	}

	width := m.chatView.Viewport.Width
	contentWidth := max(1, width-4)
	lines := wrapText(m.currentThinking, contentWidth)
	limit := min(thinkingContentHeight, max(1, m.height/5))
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	body := styles.ThinkingTitle.Render("Thinking") + "\n" + strings.Join(lines, "\n")
	return styles.ThinkingBox.Width(max(1, width-2)).Render(body)
}

func wrapText(text string, width int) []string {
	// TODO(review): uses len() (byte length) for width; multi-byte/wide chars wrap
	// incorrectly. Use lipgloss/x/ansi width helpers. See notes/review-checklist.md.
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		words := strings.Fields(raw)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) > width {
				lines = append(lines, line)
				line = word
				continue
			}
			line += " " + word
		}
		lines = append(lines, line)
	}
	return lines
}

func waitForResponse(ch chan *types.ChatResponse) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return errMsg(fmt.Errorf("response channel closed"))
		}
		return responseMsg(msg)
	}
}

func formatMsg(sender, content string, style lipgloss.Style) string {
	ts := time.Now().Format("15:04")
	return style.Render(sender) + styles.MutedText.Render("  "+ts) + "\n" + content
}

func (m *Model) SetInitialContent(content string) {
	m.chatView.Viewport.SetContent(content)
}
