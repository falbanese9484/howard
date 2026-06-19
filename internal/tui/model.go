package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

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

	var inputCmd, viewCmd tea.Cmd
	m.input.Textarea, inputCmd = m.input.Textarea.Update(msg)
	m.chatView.Viewport, viewCmd = m.chatView.Viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case responseMsg:
		return m.handleResponse(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
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
		return m.selector.View()
	}

	thinking := m.renderThinkingBox()
	if thinking != "" {
		return fmt.Sprintf("%s\n%s\n%s", m.chatView.Viewport.View(), thinking, m.input.Textarea.View())
	}
	return fmt.Sprintf("%s\n\n%s", m.chatView.Viewport.View(), m.input.Textarea.View())
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
		m.chatView.Append(formatMsg("Error", strings.TrimSpace(msg.Response), styles.AITool))
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
			lipgloss.NewStyle().Width(m.chatView.Viewport.Width).Render(strings.Join(all, "\n")),
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

		prompt := strings.TrimSpace(m.input.Textarea.Value())
		if prompt == "" {
			return m, nil
		}
		if prompt == "exit" {
			return m, tea.Quit
		}

		m.clipboard.Push(prompt)

		m.currentResp = ""
		m.currentThinking = ""
		m.isResponding = true
		m.updateLayout()
		m.chatView.Append(styles.User.Render("You: ") + prompt)
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
	mainWidth := m.width * 2 / 3
	m.chatView.Viewport.Width = mainWidth
	m.input.Textarea.SetWidth(mainWidth)

	reserved := m.input.Textarea.Height() + 4
	if m.currentThinking != "" {
		reserved += thinkingContentHeight + 3
	}
	m.chatView.Viewport.Height = max(1, m.height-reserved)
}

func (m *Model) renderThinkingBox() string {
	if strings.TrimSpace(m.currentThinking) == "" {
		return ""
	}

	width := max(20, m.chatView.Viewport.Width)
	contentWidth := max(10, width-4)
	lines := wrapText(m.currentThinking, contentWidth)
	if len(lines) > thinkingContentHeight {
		lines = lines[len(lines)-thinkingContentHeight:]
	}

	body := styles.ThinkingTitle.Render("Thinking") + "\n" + strings.Join(lines, "\n")
	return styles.ThinkingBox.Width(width - 2).Render(body)
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
	return style.Render(fmt.Sprintf("[%s] %s:", ts, sender)) + " " + content
}

func (m *Model) SetInitialContent(content string) {
	m.chatView.Viewport.SetContent(content)
}
