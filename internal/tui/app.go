package tui

import (
	"context"
	"encoding/json"
	"strings"

	"gar/internal/llm"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	defaultAppWidth         = 100
	defaultInspectorWidth   = 36
	minimumChatPanelWidth   = 40
	minimumInspectorVisible = 22
	defaultMaxTokens        = 1024
)

// StreamRunner executes one request and returns a streaming channel.
type StreamRunner interface {
	Run(ctx context.Context, req *llm.Request) (<-chan llm.Event, error)
}

// AppConfig configures the root BubbleTea model.
type AppConfig struct {
	Version       string
	ModelName     string
	CWD           string
	SessionID     string
	ThemeName     string
	ShowInspector bool
	Runner        StreamRunner
	MaxTokens     int
	Tools         []llm.ToolSpec
}

// StreamEventMsg wraps one llm event for app updates.
type StreamEventMsg struct {
	Event llm.Event
}

type streamReadMsg struct {
	Event  llm.Event
	Closed bool
}

// App is the root TUI model.
type App struct {
	theme         Theme
	showInspector bool

	runner    StreamRunner
	modelName string
	maxTokens int
	tools     []llm.ToolSpec

	width  int
	height int

	status    StatusModel
	chat      ChatModel
	input     InputModel
	inspector InspectorModel

	conversation    []llm.Message
	assistantBuffer strings.Builder
	activeStream    <-chan llm.Event
}

// NewApp constructs the root TUI model with defaults.
func NewApp(cfg AppConfig) *App {
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	model := &App{
		theme:         ResolveTheme(cfg.ThemeName),
		showInspector: cfg.ShowInspector,
		runner:        cfg.Runner,
		modelName:     strings.TrimSpace(cfg.ModelName),
		maxTokens:     maxTokens,
		tools:         cloneToolSpecs(cfg.Tools),
		status:        NewStatusModel(cfg.Version, cfg.ModelName, cfg.CWD, cfg.SessionID),
		chat:          NewChatModel(0),
		input:         NewInputModel(">", "Type message and press Enter"),
		inspector:     NewInspectorModel(),
	}

	if model.width == 0 {
		model.width = defaultAppWidth
	}
	model.status.SetState("idle")
	return model
}

// Init starts background commands if needed.
func (m *App) Init() tea.Cmd {
	return nil
}

// Update applies state changes from user input and runtime events.
func (m *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.SetViewportHeight(m.chatViewportHeight())
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
		if m.handleChatScrollKey(msg) {
			return m, nil
		}

		if submitted := m.input.HandleKey(msg); submitted {
			content := strings.TrimSpace(m.input.Value())
			m.input.Clear()
			if content == "" {
				return m, nil
			}

			m.chat.Append("user", content)
			m.inspector.IncrementTurn()
			m.appendUserMessage(content)
			return m, m.startRunCommand()
		}
		return m, nil

	case StreamEventMsg:
		m.consumeEvent(msg.Event)
		if m.activeStream != nil {
			return m, readStreamEventCommand(m.activeStream)
		}
		return m, nil

	case streamReadMsg:
		if msg.Closed {
			m.activeStream = nil
			return m, nil
		}
		m.consumeEvent(msg.Event)
		if m.activeStream != nil {
			return m, readStreamEventCommand(m.activeStream)
		}
		return m, nil

	case llm.Event:
		m.consumeEvent(msg)
		if m.activeStream != nil {
			return m, readStreamEventCommand(m.activeStream)
		}
		return m, nil
	}

	return m, nil
}

// View renders status bar, chat, optional inspector, and input line.
func (m *App) View() string {
	width := m.width
	if width <= 0 {
		width = defaultAppWidth
	}

	statusLine := m.status.Render(width, m.theme)
	body := m.renderBody(width)
	inputLine := m.input.Render(width, m.theme)
	return strings.Join([]string{statusLine, body, inputLine}, "\n")
}

func (m *App) startRunCommand() tea.Cmd {
	if m.runner == nil {
		return nil
	}
	if m.activeStream != nil {
		m.appendErrorMessage("agent is busy")
		return nil
	}

	request := &llm.Request{
		Model:     m.modelName,
		Messages:  cloneMessages(m.conversation),
		Tools:     cloneToolSpecs(m.tools),
		MaxTokens: m.maxTokens,
	}

	stream, err := m.runner.Run(context.Background(), request)
	if err != nil {
		m.appendErrorMessage(err.Error())
		return nil
	}

	m.activeStream = stream
	m.status.SetState("streaming")
	m.inspector.SetState("streaming")
	return readStreamEventCommand(stream)
}

func readStreamEventCommand(stream <-chan llm.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-stream
		if !ok {
			return streamReadMsg{Closed: true}
		}
		return streamReadMsg{Event: event}
	}
}

func (m *App) consumeEvent(ev llm.Event) {
	switch ev.Type {
	case llm.EventStart:
		m.status.SetState("streaming")
		m.inspector.SetState("streaming")
	case llm.EventContentBlockStart:
		if ev.ContentBlockStart != nil && ev.ContentBlockStart.Type == "text" && ev.ContentBlockStart.Text != "" {
			m.assistantBuffer.WriteString(ev.ContentBlockStart.Text)
			m.status.SetState("streaming")
			m.inspector.SetState("streaming")
		}
	case llm.EventTextDelta:
		m.assistantBuffer.WriteString(ev.TextDelta)
		m.status.SetState("streaming")
		m.inspector.SetState("streaming")
	case llm.EventToolCallStart:
		if ev.ToolCall != nil {
			m.inspector.RecordToolCall(ev.ToolCall.Name)
			m.status.SetState("tool_executing")
			m.inspector.SetState("tool_executing")
		}
	case llm.EventUsage:
		if ev.Usage != nil {
			m.inspector.SetUsage(*ev.Usage)
		}
	case llm.EventDone:
		if ev.Done != nil && ev.Done.Reason == llm.StopReasonToolUse {
			// tool_use is an intermediate terminal from provider turn; agent loop continues.
			m.flushAssistantBuffer()
			m.status.SetState("streaming")
			m.inspector.SetState("streaming")
			return
		}
		m.flushAssistantBuffer()
		m.status.SetState("idle")
		m.inspector.SetState("idle")
		m.activeStream = nil
	case llm.EventError:
		m.flushAssistantBuffer()
		errText := "stream error"
		if ev.Err != nil {
			errText = ev.Err.Error()
		}
		m.appendErrorMessage(errText)
		m.activeStream = nil
	}
}

func (m *App) appendUserMessage(content string) {
	m.conversation = append(m.conversation, llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{{
			Type: llm.ContentTypeText,
			Text: content,
		}},
	})
}

func (m *App) appendAssistantMessage(content string) {
	m.conversation = append(m.conversation, llm.Message{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{{
			Type: llm.ContentTypeText,
			Text: content,
		}},
	})
}

func (m *App) appendErrorMessage(errText string) {
	message := "Error: " + strings.TrimSpace(errText)
	m.chat.Append("assistant", message)
	m.status.SetState("error")
	m.inspector.SetState("error")
}

func (m *App) flushAssistantBuffer() {
	text := strings.TrimSpace(m.assistantBuffer.String())
	if text != "" {
		m.chat.Append("assistant", text)
		m.appendAssistantMessage(text)
	}
	m.assistantBuffer.Reset()
}

func (m *App) renderBody(width int) string {
	m.chat.SetViewportHeight(m.chatViewportHeight())

	if !m.showInspector {
		return m.chat.Render(width, m.theme)
	}

	inspectorWidth := defaultInspectorWidth
	if width/3 < inspectorWidth {
		inspectorWidth = width / 3
	}
	if inspectorWidth < minimumInspectorVisible {
		inspectorWidth = minimumInspectorVisible
	}

	chatWidth := width - inspectorWidth - 1
	if chatWidth < minimumChatPanelWidth {
		chatWidth = minimumChatPanelWidth
		inspectorWidth = width - chatWidth - 1
		if inspectorWidth < 0 {
			inspectorWidth = 0
		}
	}

	chatView := m.chat.Render(chatWidth, m.theme)
	if inspectorWidth <= 0 {
		return chatView
	}

	inspectorView := m.inspector.Render(inspectorWidth, m.theme)
	return lipgloss.JoinHorizontal(lipgloss.Top, chatView, inspectorView)
}

func (m *App) handleChatScrollKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyUp:
		m.chat.ScrollUp(1)
		return true
	case tea.KeyDown:
		m.chat.ScrollDown(1)
		return true
	case tea.KeyPgUp:
		m.chat.PageUp()
		return true
	case tea.KeyPgDown:
		m.chat.PageDown()
		return true
	case tea.KeyHome:
		m.chat.ScrollToTop()
		return true
	case tea.KeyEnd:
		m.chat.ScrollToBottom()
		return true
	default:
		return false
	}
}

func (m *App) chatViewportHeight() int {
	if m.height <= 0 {
		return 0
	}

	const nonBodyRows = 2 // status + input
	bodyHeight := m.height - nonBodyRows
	if bodyHeight < 1 {
		return 1
	}

	contentHeight := bodyHeight - m.theme.PanelStyle.GetVerticalFrameSize()
	if contentHeight < 1 {
		return 1
	}
	return contentHeight
}

func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		copyMsg := llm.Message{
			Role:      message.Role,
			Content:   append([]llm.ContentBlock(nil), message.Content...),
			ToolCalls: cloneToolCalls(message.ToolCalls),
		}
		if message.ToolResult != nil {
			result := *message.ToolResult
			copyMsg.ToolResult = &result
		}
		cloned = append(cloned, copyMsg)
	}
	return cloned
}

func cloneToolCalls(calls []llm.ToolCall) []llm.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	cloned := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		copyCall := call
		copyCall.Arguments = append(json.RawMessage(nil), call.Arguments...)
		cloned = append(cloned, copyCall)
	}
	return cloned
}

func cloneToolSpecs(specs []llm.ToolSpec) []llm.ToolSpec {
	if len(specs) == 0 {
		return nil
	}

	cloned := make([]llm.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		copySpec := spec
		copySpec.Schema = append(json.RawMessage(nil), spec.Schema...)
		cloned = append(cloned, copySpec)
	}
	return cloned
}
