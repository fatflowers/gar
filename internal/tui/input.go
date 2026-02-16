package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InputModel stores a single-line prompt buffer.
type InputModel struct {
	prompt      string
	placeholder string
	value       string
}

// NewInputModel constructs the input state.
func NewInputModel(prompt, placeholder string) InputModel {
	p := strings.TrimSpace(prompt)
	if p == "" {
		p = ">"
	}
	return InputModel{
		prompt:      p,
		placeholder: strings.TrimSpace(placeholder),
	}
}

// Value returns current raw input text.
func (m InputModel) Value() string {
	return m.value
}

// SetValue replaces input text.
func (m *InputModel) SetValue(value string) {
	m.value = value
}

// Clear resets input text.
func (m *InputModel) Clear() {
	m.value = ""
}

// HandleKey mutates input state and reports submit key.
func (m *InputModel) HandleKey(msg tea.KeyMsg) (submitted bool) {
	switch msg.Type {
	case tea.KeyEnter:
		return true
	case tea.KeyBackspace, tea.KeyDelete:
		if m.value == "" {
			return false
		}
		runes := []rune(m.value)
		m.value = string(runes[:len(runes)-1])
		return false
	case tea.KeySpace:
		m.value += " "
		return false
	}

	if len(msg.Runes) > 0 {
		m.value += string(msg.Runes)
	}
	return false
}

// Render draws the input line.
func (m InputModel) Render(width int, theme Theme) string {
	value := m.value
	valueStyle := theme.InputTextStyle
	if strings.TrimSpace(value) == "" {
		value = m.placeholder
		valueStyle = theme.InputPlaceholderTextStyle
	}

	line := theme.InputPromptStyle.Render(m.prompt+" ") + valueStyle.Render(value)
	if width > 0 {
		return lipgloss.NewStyle().Width(width).Render(line)
	}
	return line
}
