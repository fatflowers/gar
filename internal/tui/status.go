package tui

import (
	"fmt"
	"strings"
)

// StatusModel renders the top status bar.
type StatusModel struct {
	Version   string
	ModelName string
	CWD       string
	SessionID string
	State     string
}

// NewStatusModel constructs status data for rendering.
func NewStatusModel(version, modelName, cwd, sessionID string) StatusModel {
	return StatusModel{
		Version:   strings.TrimSpace(version),
		ModelName: strings.TrimSpace(modelName),
		CWD:       strings.TrimSpace(cwd),
		SessionID: strings.TrimSpace(sessionID),
		State:     "idle",
	}
}

// SetState updates the runtime state token.
func (m *StatusModel) SetState(state string) {
	m.State = strings.TrimSpace(state)
	if m.State == "" {
		m.State = "idle"
	}
}

// Render draws a one-line status bar.
func (m StatusModel) Render(width int, theme Theme) string {
	parts := []string{
		"gar " + fallbackText(m.Version, "dev"),
		fallbackText(m.ModelName, "unknown-model"),
		fallbackText(m.CWD, "unknown-cwd"),
		"session: " + fallbackText(m.SessionID, "new"),
		"state: " + fallbackText(m.State, "idle"),
	}
	line := strings.Join(parts, " | ")
	style := theme.StatusBarStyle
	if width > 0 {
		style = style.Width(width)
	}
	return style.Render(line)
}

func fallbackText(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func formatCostUSD(cost float64) string {
	return fmt.Sprintf("$%.4f", cost)
}
