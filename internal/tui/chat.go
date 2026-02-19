package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const defaultChatLimit = 500

// ChatMessage is one rendered chat item.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatModel stores stream messages for display.
type ChatModel struct {
	messages    []ChatMessage
	maxMessages int
	scrollTop   int

	// viewportHeight is the number of visible content lines inside the chat panel.
	// 0 means unconstrained.
	viewportHeight int
}

// NewChatModel creates a chat buffer with retention limit.
func NewChatModel(maxMessages int) ChatModel {
	limit := maxMessages
	if limit <= 0 {
		limit = defaultChatLimit
	}
	return ChatModel{maxMessages: limit}
}

// Append records one message when content is non-empty.
func (m *ChatModel) Append(role, content string) {
	text := strings.TrimSpace(content)
	if text == "" {
		return
	}
	wasAtBottom := m.isAtBottom()

	m.messages = append(m.messages, ChatMessage{
		Role:    strings.TrimSpace(role),
		Content: text,
	})

	if overflow := len(m.messages) - m.maxMessages; overflow > 0 {
		m.messages = append([]ChatMessage(nil), m.messages[overflow:]...)
	}
	if wasAtBottom {
		m.scrollToBottom()
		return
	}
	m.clampScrollTop()
}

// Messages returns a defensive copy of buffered messages.
func (m ChatModel) Messages() []ChatMessage {
	copied := make([]ChatMessage, 0, len(m.messages))
	for _, message := range m.messages {
		copied = append(copied, message)
	}
	return copied
}

// Clear removes all buffered chat messages.
func (m *ChatModel) Clear() {
	m.messages = nil
	m.scrollTop = 0
}

// SetViewportHeight configures the visible line count for chat content.
func (m *ChatModel) SetViewportHeight(height int) {
	if height < 0 {
		height = 0
	}
	m.viewportHeight = height
	m.clampScrollTop()
}

// ScrollUp moves the chat viewport up by lines.
func (m *ChatModel) ScrollUp(lines int) {
	if lines <= 0 {
		return
	}
	m.scrollTop -= lines
	m.clampScrollTop()
}

// ScrollDown moves the chat viewport down by lines.
func (m *ChatModel) ScrollDown(lines int) {
	if lines <= 0 {
		return
	}
	m.scrollTop += lines
	m.clampScrollTop()
}

// PageUp scrolls one viewport up.
func (m *ChatModel) PageUp() {
	step := m.viewportHeight
	if step <= 0 {
		step = 10
	}
	m.ScrollUp(step)
}

// PageDown scrolls one viewport down.
func (m *ChatModel) PageDown() {
	step := m.viewportHeight
	if step <= 0 {
		step = 10
	}
	m.ScrollDown(step)
}

// ScrollToTop jumps to the top of buffered chat lines.
func (m *ChatModel) ScrollToTop() {
	m.scrollTop = 0
}

// ScrollToBottom jumps to the most recent chat lines.
func (m *ChatModel) ScrollToBottom() {
	m.scrollToBottom()
}

// Render draws chat lines inside a panel.
func (m ChatModel) Render(width int, theme Theme) string {
	if len(m.messages) == 0 {
		return renderPanel(width, theme.PanelStyle, "No messages yet.")
	}

	lines := make([]string, 0, len(m.messages))
	for _, message := range m.messages {
		prefix, style := rolePrefix(message.Role, theme)
		raw := strings.Split(message.Content, "\n")
		if len(raw) == 0 {
			continue
		}
		lines = append(lines, style.Render(prefix)+" "+raw[0])
		if len(raw) > 1 {
			lines = append(lines, raw[1:]...)
		}
	}

	if m.viewportHeight > 0 && len(lines) > m.viewportHeight {
		start := m.scrollTop
		maxTop := len(lines) - m.viewportHeight
		if start < 0 {
			start = 0
		}
		if start > maxTop {
			start = maxTop
		}
		end := start + m.viewportHeight
		lines = lines[start:end]
	}

	return renderPanel(width, theme.PanelStyle, strings.Join(lines, "\n"))
}

func rolePrefix(role string, theme Theme) (string, lipgloss.Style) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		return "assistant:", theme.AssistantPrefixStyle
	case "tool":
		return "tool:", theme.ToolPrefixStyle
	default:
		return "user:", theme.UserPrefixStyle
	}
}

func renderPanel(width int, style lipgloss.Style, content string) string {
	if width > 0 {
		return style.Width(width).Render(content)
	}
	return style.Render(content)
}

func (m *ChatModel) isAtBottom() bool {
	if m.viewportHeight <= 0 {
		return true
	}
	return m.scrollTop >= m.maxScrollTop()
}

func (m *ChatModel) maxScrollTop() int {
	if m.viewportHeight <= 0 {
		return 0
	}
	maxTop := m.totalRenderedLines() - m.viewportHeight
	if maxTop < 0 {
		return 0
	}
	return maxTop
}

func (m *ChatModel) scrollToBottom() {
	m.scrollTop = m.maxScrollTop()
}

func (m *ChatModel) clampScrollTop() {
	if m.scrollTop < 0 {
		m.scrollTop = 0
		return
	}
	maxTop := m.maxScrollTop()
	if m.scrollTop > maxTop {
		m.scrollTop = maxTop
	}
}

func (m *ChatModel) totalRenderedLines() int {
	total := 0
	for _, message := range m.messages {
		total += len(strings.Split(message.Content, "\n"))
	}
	return total
}
