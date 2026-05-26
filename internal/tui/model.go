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
)

type (
	responseMsg *types.ChatResponse
	errMsg      error
)

type Model struct {
	input       *components.InputArea
	chatView    *components.ChatView
	selector    *components.ModelSelector
	bus         *chat.Bus
	provider    types.Provider
	responses   chan *types.ChatResponse
	renderer    *glamour.TermRenderer
	logger      *logger.Logger
	modelName   string
	currentResp string
	mode        mode
	err         error
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

	return &Model{
		input:     components.NewInputArea(),
		chatView:  components.NewChatView(mainWidth, cfg.Height/2),
		selector:  components.NewModelSelector(mainWidth, cfg.Height/4),
		bus:       cfg.Bus,
		provider:  cfg.Provider,
		responses: cfg.Responses,
		renderer:  renderer,
		logger:    cfg.Logger,
		modelName: cfg.ModelName,
		mode:      modeChat,
	}
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
	return fmt.Sprintf("%s\n\n%s", m.chatView.Viewport.View(), m.input.Textarea.View())
}

func (m *Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	mainWidth := msg.Width * 2 / 3
	m.chatView.Viewport.Width = mainWidth
	m.input.Textarea.SetWidth(mainWidth)
	m.chatView.Viewport.Height = msg.Height - m.input.Textarea.Height() - 4
	if len(m.chatView.Messages) > 0 {
		m.chatView.Render()
	}
	return m, nil
}

func (m *Model) handleResponse(msg responseMsg) (tea.Model, tea.Cmd) {
	if msg == nil {
		return m, nil
	}
	if msg.Response != "" {
		m.currentResp += msg.Response
		rendered, _ := m.renderer.Render(m.currentResp)
		all := append(m.chatView.Messages, formatMsg(m.modelName, rendered, styles.AI))
		m.chatView.Viewport.SetContent(
			lipgloss.NewStyle().Width(m.chatView.Viewport.Width).Render(strings.Join(all, "\n")),
		)
		m.chatView.Viewport.GotoBottom()
	}
	if !msg.Done {
		return m, waitForResponse(m.responses)
	}

	rendered, _ := m.renderer.Render(m.currentResp)
	m.chatView.Append(formatMsg(m.modelName, rendered, styles.AI))
	m.currentResp = ""
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEscape:
		if m.mode == modeModelSelect {
			m.selector.Toggle()
			m.mode = modeChat
			return m, nil
		}
		return m, tea.Quit
	case tea.KeyEnter:
		prompt := m.input.Textarea.Value()
		m.chatView.Append(styles.User.Render("You: ") + prompt)
		m.input.Textarea.Reset()
		req := m.provider.GenerateRequest(prompt)
		go m.bus.Send(req)
		return m, waitForResponse(m.responses)
	case tea.KeyCtrlF:
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
