package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme contains style tokens used by the terminal UI.
type Theme struct {
	Name                      string
	StatusBarStyle            lipgloss.Style
	PanelStyle                lipgloss.Style
	InspectorStyle            lipgloss.Style
	UserPrefixStyle           lipgloss.Style
	AssistantPrefixStyle      lipgloss.Style
	ToolPrefixStyle           lipgloss.Style
	InputPromptStyle          lipgloss.Style
	InputTextStyle            lipgloss.Style
	InputPlaceholderTextStyle lipgloss.Style
}

// ResolveTheme returns the configured theme or the dark default.
func ResolveTheme(name string) Theme {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "light":
		return newLightTheme()
	default:
		return newDarkTheme()
	}
}

func newDarkTheme() Theme {
	border := lipgloss.Color("63")
	muted := lipgloss.Color("245")
	return Theme{
		Name: "dark",
		StatusBarStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("63")).
			Padding(0, 1),
		PanelStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(0, 1),
		InspectorStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(0, 1),
		UserPrefixStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
		AssistantPrefixStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true),
		ToolPrefixStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true),
		InputPromptStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
		InputTextStyle:       lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		InputPlaceholderTextStyle: lipgloss.NewStyle().
			Foreground(muted).
			Italic(true),
	}
}

func newLightTheme() Theme {
	border := lipgloss.Color("246")
	muted := lipgloss.Color("240")
	return Theme{
		Name: "light",
		StatusBarStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("16")).
			Background(lipgloss.Color("189")).
			Padding(0, 1),
		PanelStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(0, 1),
		InspectorStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(0, 1),
		UserPrefixStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("25")).Bold(true),
		AssistantPrefixStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("94")).Bold(true),
		ToolPrefixStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("31")).Bold(true),
		InputPromptStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("25")).Bold(true),
		InputTextStyle:       lipgloss.NewStyle().Foreground(lipgloss.Color("16")),
		InputPlaceholderTextStyle: lipgloss.NewStyle().
			Foreground(muted).
			Italic(true),
	}
}
