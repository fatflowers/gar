package tui

import (
	"fmt"
	"sort"
	"strings"

	"gar/internal/llm"
)

// InspectorModel renders transparent runtime stats.
type InspectorModel struct {
	State      string
	Turn       int
	Usage      llm.Usage
	CostUSD    float64
	ToolCounts map[string]int
}

// NewInspectorModel constructs inspector defaults.
func NewInspectorModel() InspectorModel {
	return InspectorModel{
		State:      "idle",
		ToolCounts: make(map[string]int),
	}
}

// SetState updates runtime state label.
func (m *InspectorModel) SetState(state string) {
	trimmed := strings.TrimSpace(state)
	if trimmed == "" {
		trimmed = "idle"
	}
	m.State = trimmed
}

// IncrementTurn updates turn counter.
func (m *InspectorModel) IncrementTurn() {
	m.Turn++
}

// SetUsage stores latest usage snapshot.
func (m *InspectorModel) SetUsage(usage llm.Usage) {
	m.Usage = usage
	m.CostUSD = usage.CostUSD
}

// RecordToolCall increments tool call count.
func (m *InspectorModel) RecordToolCall(toolName string) {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "unknown"
	}
	m.ToolCounts[name]++
}

// Render draws the inspector panel.
func (m InspectorModel) Render(width int, theme Theme) string {
	lines := []string{
		"Status: " + m.State,
		fmt.Sprintf("Turn: %d", m.Turn),
		fmt.Sprintf("Tokens: %d", m.Usage.TokenCount()),
		"Cost: " + formatCostUSD(m.CostUSD),
		"Tools:",
	}

	if len(m.ToolCounts) == 0 {
		lines = append(lines, "  none")
	} else {
		names := make([]string, 0, len(m.ToolCounts))
		for name := range m.ToolCounts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			lines = append(lines, fmt.Sprintf("  %s (%d)", name, m.ToolCounts[name]))
		}
	}

	return renderPanel(width, theme.InspectorStyle, strings.Join(lines, "\n"))
}
