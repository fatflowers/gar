package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentsession "gar/internal/agent/session"
	"gar/internal/agentapp"
	"gar/internal/llm"
	sessionstore "gar/internal/session"

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
	SessionStore  *sessionstore.Store
}

// StreamEventMsg wraps one llm event for app updates.
type StreamEventMsg struct {
	Event llm.Event
}

type streamReadMsg struct {
	Event  llm.Event
	Closed bool
}

type selectorKind string

const (
	selectorKindResume selectorKind = "resume"
	selectorKindTree   selectorKind = "tree"
)

type selectorItem struct {
	Value string
	Label string
}

type selectorState struct {
	Kind   selectorKind
	Title  string
	Items  []selectorItem
	Cursor int
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

	session         *agentsession.AgentSession
	sessionInitErr  error
	selector        *selectorState
	assistantBuffer strings.Builder
	activeStream    <-chan llm.Event
}

// NewApp constructs the root TUI model with defaults.
func NewApp(cfg AppConfig) *App {
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	sessionID := strings.TrimSpace(cfg.SessionID)
	if sessionID == "" {
		sessionID = time.Now().UTC().Format("20060102-150405")
	}

	model := &App{
		theme:         ResolveTheme(cfg.ThemeName),
		showInspector: cfg.ShowInspector,
		runner:        cfg.Runner,
		modelName:     strings.TrimSpace(cfg.ModelName),
		maxTokens:     maxTokens,
		tools:         cloneToolSpecs(cfg.Tools),
		status:        NewStatusModel(cfg.Version, cfg.ModelName, cfg.CWD, sessionID),
		chat:          NewChatModel(0),
		input:         NewInputModel(">", "Type message and press Enter"),
		inspector:     NewInspectorModel(),
	}

	if model.width == 0 {
		model.width = defaultAppWidth
	}

	if cfg.Runner != nil {
		sessionModel, err := agentsession.New(context.Background(), agentsession.Config{
			Runner:    cfg.Runner,
			Store:     cfg.SessionStore,
			SessionID: sessionID,
			Model:     strings.TrimSpace(cfg.ModelName),
			MaxTokens: maxTokens,
			Tools:     cfg.Tools,
			Meta: map[string]any{
				"model": strings.TrimSpace(cfg.ModelName),
				"cwd":   strings.TrimSpace(cfg.CWD),
			},
		})
		if err != nil {
			model.sessionInitErr = err
		} else {
			model.session = sessionModel
			model.rebuildChatFromSession()
		}
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
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.selector != nil {
				return m, m.cancelSelector()
			}
			if strings.TrimSpace(m.input.Value()) == "" && m.activeStream == nil {
				return m, tea.Quit
			}
		}

		if m.selector != nil {
			return m, m.handleSelectorKey(msg)
		}
		if m.handleChatScrollKey(msg) {
			return m, nil
		}

		if msg.Type == tea.KeyEnter && (msg.Alt || msg.String() == "alt+enter") {
			content := strings.TrimSpace(m.input.Value())
			m.input.Clear()
			return m, m.handleInputSubmit(content, true)
		}

		if submitted := m.input.HandleKey(msg); submitted {
			content := strings.TrimSpace(m.input.Value())
			m.input.Clear()
			return m, m.handleInputSubmit(content, false)
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
			if m.session != nil {
				if err := m.session.Finalize(context.Background()); err != nil {
					m.appendErrorMessage(err.Error())
				}
			}
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

func (m *App) handleInputSubmit(content string, followUp bool) tea.Cmd {
	if content == "" {
		return nil
	}
	if m.sessionInitErr != nil {
		m.appendErrorMessage(m.sessionInitErr.Error())
		return nil
	}
	if m.session == nil {
		m.appendErrorMessage("session is not initialized")
		return nil
	}

	if strings.HasPrefix(content, "/") {
		return m.handleSlashCommand(content)
	}

	if m.activeStream != nil {
		var err error
		if followUp {
			err = m.session.QueueFollowUp(content)
		} else {
			err = m.session.QueueSteer(content)
		}
		if err != nil {
			m.appendErrorMessage(err.Error())
			return nil
		}
		mode := "steer"
		if followUp {
			mode = "follow-up"
		}
		m.chat.Append("assistant", fmt.Sprintf("Queued %s message.", mode))
		return nil
	}

	m.chat.Append("user", content)
	m.inspector.IncrementTurn()

	stream, err := m.session.Submit(context.Background(), content)
	if err != nil {
		m.appendErrorMessage(err.Error())
		return nil
	}
	return m.startStream(stream)
}

func (m *App) handleSlashCommand(content string) tea.Cmd {
	return agentapp.ExecuteSlashCommand(content, agentapp.CommandEnv{
		Session:      m.session,
		ActiveStream: m.activeStream != nil,
		OpenResumeSelector: func() tea.Cmd {
			return m.openResumeSelector()
		},
		OpenTreeSelector: func() tea.Cmd {
			return m.openTreeSelector()
		},
		RebuildChatFromSession: func() {
			m.rebuildChatFromSession()
		},
		RefreshSessionStatus: func() {
			m.refreshSessionStatus()
		},
		GetInputValue: func() string {
			return m.input.Value()
		},
		SetInputValue: func(value string) {
			m.input.SetValue(value)
		},
		AppendAssistant: func(text string) {
			m.chat.Append("assistant", text)
		},
		AppendError: func(errText string) {
			m.appendErrorMessage(errText)
		},
	})
}

func (m *App) openResumeSelector() tea.Cmd {
	infos, err := m.session.ListSessions(context.Background())
	if err != nil {
		m.appendErrorMessage(err.Error())
		return nil
	}
	if len(infos) == 0 {
		m.chat.Append("assistant", "No sessions found.")
		return nil
	}

	items := make([]selectorItem, 0, len(infos))
	current := m.session.SessionID()
	cursor := 0
	for index, info := range infos {
		label := fmt.Sprintf("%s  (%s)", info.ID, info.UpdatedAt.Format(time.DateTime))
		if info.ID == current {
			label = label + "  [current]"
			cursor = index
		}
		items = append(items, selectorItem{
			Value: info.ID,
			Label: label,
		})
	}

	m.selector = &selectorState{
		Kind:   selectorKindResume,
		Title:  "Select Session",
		Items:  items,
		Cursor: cursor,
	}
	return nil
}

func (m *App) openTreeSelector() tea.Cmd {
	lines := m.session.TreeLines()
	if len(lines) == 0 {
		m.chat.Append("assistant", "Session tree is empty.")
		return nil
	}

	items := make([]selectorItem, 0, len(lines))
	cursor := 0
	for _, line := range lines {
		id, isCurrent, ok := parseTreeLine(line)
		if !ok {
			continue
		}
		label := strings.TrimSpace(line)
		items = append(items, selectorItem{
			Value: id,
			Label: label,
		})
		if isCurrent {
			cursor = len(items) - 1
		}
	}
	if len(items) == 0 {
		m.chat.Append("assistant", "Session tree is empty.")
		return nil
	}

	m.selector = &selectorState{
		Kind:   selectorKindTree,
		Title:  "Select Tree Entry",
		Items:  items,
		Cursor: cursor,
	}
	return nil
}

func (m *App) handleSelectorKey(msg tea.KeyMsg) tea.Cmd {
	if m.selector == nil {
		return nil
	}

	switch msg.Type {
	case tea.KeyEsc:
		return m.cancelSelector()
	case tea.KeyUp:
		m.selector.Cursor--
		if m.selector.Cursor < 0 {
			m.selector.Cursor = len(m.selector.Items) - 1
		}
		return nil
	case tea.KeyDown:
		m.selector.Cursor++
		if m.selector.Cursor >= len(m.selector.Items) {
			m.selector.Cursor = 0
		}
		return nil
	case tea.KeyEnter:
		return m.confirmSelector()
	default:
		return nil
	}
}

func (m *App) cancelSelector() tea.Cmd {
	if m.selector == nil {
		return nil
	}
	m.selector = nil
	m.chat.Append("assistant", "Selection cancelled.")
	return nil
}

func (m *App) confirmSelector() tea.Cmd {
	if m.selector == nil || len(m.selector.Items) == 0 {
		m.selector = nil
		return nil
	}
	selected := m.selector.Items[m.selector.Cursor]
	kind := m.selector.Kind
	m.selector = nil

	switch kind {
	case selectorKindResume:
		if err := m.session.SwitchSession(context.Background(), selected.Value); err != nil {
			m.appendErrorMessage(err.Error())
			return nil
		}
		m.rebuildChatFromSession()
		m.refreshSessionStatus()
		m.chat.Append("assistant", "Resumed session "+selected.Value+".")
	case selectorKindTree:
		if err := m.session.SwitchBranch(selected.Value); err != nil {
			m.appendErrorMessage(err.Error())
			return nil
		}
		m.rebuildChatFromSession()
		m.chat.Append("assistant", "Switched branch to "+selected.Value+".")
	}

	return nil
}

func (m *App) startRunCommand() tea.Cmd {
	if m.runner == nil {
		return nil
	}
	if m.activeStream != nil {
		m.appendErrorMessage("agent is busy")
		return nil
	}
	if m.session != nil {
		stream, err := m.session.Run(context.Background())
		if err != nil {
			m.appendErrorMessage(err.Error())
			return nil
		}
		return m.startStream(stream)
	}
	return nil
}

func (m *App) startStream(stream <-chan llm.Event) tea.Cmd {
	if stream == nil {
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
	if m.session != nil {
		if err := m.session.RecordEvent(context.Background(), ev); err != nil {
			m.appendErrorMessage(err.Error())
		}
	}

	switch ev.Type {
	case llm.EventStart:
		m.status.SetState("streaming")
		m.inspector.SetState("streaming")
	case llm.EventQueuedMessage:
		if ev.Message == nil || ev.Message.Role != llm.RoleUser {
			return
		}
		text := strings.TrimSpace(messageText(*ev.Message))
		if text == "" {
			return
		}
		m.chat.Append("user", text)
		m.inspector.IncrementTurn()
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
	}
	m.assistantBuffer.Reset()
}

func (m *App) rebuildChatFromSession() {
	if m.session == nil {
		return
	}
	m.chat.Clear()
	for _, message := range m.session.Messages() {
		switch message.Role {
		case llm.RoleUser:
			text := strings.TrimSpace(messageText(message))
			if text != "" {
				m.chat.Append("user", text)
			}
		case llm.RoleAssistant:
			text := strings.TrimSpace(messageText(message))
			if text != "" {
				m.chat.Append("assistant", text)
			}
		case llm.RoleTool:
			if message.ToolResult == nil {
				continue
			}
			content := strings.TrimSpace(message.ToolResult.Content)
			if content == "" {
				content = "(empty)"
			}
			m.chat.Append("tool", fmt.Sprintf("%s: %s", message.ToolResult.ToolName, content))
		}
	}
}

func (m *App) refreshSessionStatus() {
	if m.session == nil {
		return
	}
	m.status.SessionID = strings.TrimSpace(m.session.SessionID())
}

func (m *App) renderBody(width int) string {
	m.chat.SetViewportHeight(m.chatViewportHeight())
	if m.selector != nil {
		return m.renderSelectorBody(width)
	}

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

func (m *App) renderSelectorBody(width int) string {
	selectorView := m.renderSelectorPanel(width)
	if !m.showInspector {
		return selectorView
	}

	inspectorWidth := defaultInspectorWidth
	if width/3 < inspectorWidth {
		inspectorWidth = width / 3
	}
	if inspectorWidth < minimumInspectorVisible {
		inspectorWidth = minimumInspectorVisible
	}

	selectorWidth := width - inspectorWidth - 1
	if selectorWidth < minimumChatPanelWidth {
		selectorWidth = minimumChatPanelWidth
		inspectorWidth = width - selectorWidth - 1
		if inspectorWidth < 0 {
			inspectorWidth = 0
		}
	}

	selectorView = m.renderSelectorPanel(selectorWidth)
	if inspectorWidth <= 0 {
		return selectorView
	}
	inspectorView := m.inspector.Render(inspectorWidth, m.theme)
	return lipgloss.JoinHorizontal(lipgloss.Top, selectorView, inspectorView)
}

func (m *App) renderSelectorPanel(width int) string {
	if m.selector == nil || len(m.selector.Items) == 0 {
		return renderPanel(width, m.theme.PanelStyle, "No selectable items.")
	}
	lines := make([]string, 0, len(m.selector.Items)+2)
	lines = append(lines, m.selector.Title)
	lines = append(lines, "Use ↑/↓ to navigate, Enter to confirm, Esc to cancel.")
	for index, item := range m.selector.Items {
		prefix := "  "
		if index == m.selector.Cursor {
			prefix = "> "
		}
		lines = append(lines, prefix+item.Label)
	}
	return renderPanel(width, m.theme.PanelStyle, strings.Join(lines, "\n"))
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

func messageText(message llm.Message) string {
	if len(message.Content) == 0 {
		if message.ToolResult != nil {
			return strings.TrimSpace(message.ToolResult.Content)
		}
		return ""
	}
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if block.Type != llm.ContentTypeText {
			continue
		}
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func parseTreeLine(line string) (id string, isCurrent bool, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return "", false, false
	}

	if strings.HasPrefix(trimmed, "*") {
		isCurrent = true
		trimmed = strings.TrimLeft(strings.TrimPrefix(trimmed, "*"), " \t")
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false, false
	}

	candidate := strings.TrimSpace(fields[0])
	if candidate == "" {
		return "", false, false
	}
	for _, r := range candidate {
		if r < '0' || r > '9' {
			return "", false, false
		}
	}
	return candidate, isCurrent, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
