package agentapp

import (
	"context"

	agentsession "gar/internal/agent/session"
	sessionstore "gar/internal/session"

	tea "github.com/charmbracelet/bubbletea"
)

// SessionController is the shared command-facing session runtime contract.
type SessionController interface {
	Stats() agentsession.Stats
	SessionName() string
	SetSessionName(ctx context.Context, name string) error
	NewSession(ctx context.Context, requestedID string) (string, error)
	ListSessions(ctx context.Context) ([]sessionstore.SessionInfo, error)
	SessionID() string
	SwitchSession(ctx context.Context, sessionID string) error
	SwitchBranch(targetID string) error
	Compact(ctx context.Context, keepMessages int, instructions string) (agentsession.CompactionResult, error)
	SteeringQueued() []string
	FollowUpQueued() []string
	ClearQueue() (steering []string, followUp []string)
}

// CommandEnv provides adapter hooks so command runtime stays UI-framework agnostic.
type CommandEnv struct {
	Session SessionController

	ActiveStream bool

	OpenResumeSelector func() tea.Cmd
	OpenTreeSelector   func() tea.Cmd

	RebuildChatFromSession func()
	RefreshSessionStatus   func()

	GetInputValue func() string
	SetInputValue func(value string)

	AppendAssistant func(text string)
	AppendError     func(errText string)
}
