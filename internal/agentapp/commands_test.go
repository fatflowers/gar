package agentapp

import (
	"context"
	"strings"
	"testing"
	"time"

	agentsession "gar/internal/agent/session"
	sessionstore "gar/internal/session"
)

type fakeSession struct {
	stats     agentsession.Stats
	name      string
	sessionID string

	newSessionID string
	switchID     string
	branchID     string

	listInfos []sessionstore.SessionInfo

	compactResult agentsession.CompactionResult

	steering []string
	followUp []string
}

func (f *fakeSession) Stats() agentsession.Stats { return f.stats }
func (f *fakeSession) SessionName() string       { return f.name }
func (f *fakeSession) SetSessionName(ctx context.Context, name string) error {
	_ = ctx
	f.name = strings.TrimSpace(name)
	return nil
}
func (f *fakeSession) NewSession(ctx context.Context, requestedID string) (string, error) {
	_ = ctx
	if strings.TrimSpace(requestedID) != "" {
		f.newSessionID = strings.TrimSpace(requestedID)
	} else if f.newSessionID == "" {
		f.newSessionID = "generated"
	}
	f.sessionID = f.newSessionID
	return f.newSessionID, nil
}
func (f *fakeSession) ListSessions(ctx context.Context) ([]sessionstore.SessionInfo, error) {
	_ = ctx
	return append([]sessionstore.SessionInfo(nil), f.listInfos...), nil
}
func (f *fakeSession) SessionID() string { return f.sessionID }
func (f *fakeSession) SwitchSession(ctx context.Context, sessionID string) error {
	_ = ctx
	f.switchID = strings.TrimSpace(sessionID)
	f.sessionID = f.switchID
	return nil
}
func (f *fakeSession) SwitchBranch(targetID string) error {
	f.branchID = strings.TrimSpace(targetID)
	return nil
}
func (f *fakeSession) Compact(ctx context.Context, keepMessages int, instructions string) (agentsession.CompactionResult, error) {
	_ = ctx
	_ = keepMessages
	_ = instructions
	if f.compactResult.DroppedMessages == 0 {
		f.compactResult.DroppedMessages = 1
	}
	return f.compactResult, nil
}
func (f *fakeSession) SteeringQueued() []string { return append([]string(nil), f.steering...) }
func (f *fakeSession) FollowUpQueued() []string { return append([]string(nil), f.followUp...) }
func (f *fakeSession) ClearQueue() (steering []string, followUp []string) {
	steering = append([]string(nil), f.steering...)
	followUp = append([]string(nil), f.followUp...)
	f.steering = nil
	f.followUp = nil
	return steering, followUp
}

func TestExecuteSlashCommandHelp(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	var assistant []string
	cmd := ExecuteSlashCommand("/help", CommandEnv{
		Session: session,
		AppendAssistant: func(text string) {
			assistant = append(assistant, text)
		},
	})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if len(assistant) != 1 || !strings.Contains(assistant[0], "/session") || !strings.Contains(assistant[0], "/dequeue") {
		t.Fatalf("assistant output = %#v, want slash command list", assistant)
	}
}

func TestExecuteSlashCommandResumeLatestChoosesNonCurrent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	session := &fakeSession{
		sessionID: "current",
		listInfos: []sessionstore.SessionInfo{
			{ID: "current", UpdatedAt: now},
			{ID: "target", UpdatedAt: now.Add(-time.Minute)},
		},
	}
	var rebuildCount int
	var refreshCount int

	_ = ExecuteSlashCommand("/resume latest", CommandEnv{
		Session: session,
		RebuildChatFromSession: func() {
			rebuildCount++
		},
		RefreshSessionStatus: func() {
			refreshCount++
		},
	})

	if session.switchID != "target" {
		t.Fatalf("switchID = %q, want target", session.switchID)
	}
	if rebuildCount != 1 || refreshCount != 1 {
		t.Fatalf("rebuild=%d refresh=%d, want 1/1", rebuildCount, refreshCount)
	}
}

func TestExecuteSlashCommandTreeWithArgSwitchesBranch(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	var rebuildCount int

	_ = ExecuteSlashCommand("/tree 000123", CommandEnv{
		Session: session,
		RebuildChatFromSession: func() {
			rebuildCount++
		},
	})

	if session.branchID != "000123" {
		t.Fatalf("branchID = %q, want 000123", session.branchID)
	}
	if rebuildCount != 1 {
		t.Fatalf("rebuildCount = %d, want 1", rebuildCount)
	}
}

func TestExecuteSlashCommandQueueAndDequeue(t *testing.T) {
	t.Parallel()

	session := &fakeSession{
		steering: []string{"a"},
		followUp: []string{"b"},
	}
	inputValue := "tail"
	var assistant []string

	_ = ExecuteSlashCommand("/queue", CommandEnv{
		Session: session,
		AppendAssistant: func(text string) {
			assistant = append(assistant, text)
		},
	})
	if len(assistant) != 1 || !strings.Contains(assistant[0], "steer: a") || !strings.Contains(assistant[0], "follow-up: b") {
		t.Fatalf("queue output = %#v, want queued lines", assistant)
	}

	assistant = nil
	_ = ExecuteSlashCommand("/dequeue", CommandEnv{
		Session: session,
		GetInputValue: func() string {
			return inputValue
		},
		SetInputValue: func(value string) {
			inputValue = value
		},
		AppendAssistant: func(text string) {
			assistant = append(assistant, text)
		},
	})
	if !strings.Contains(inputValue, "a") || !strings.Contains(inputValue, "b") || !strings.Contains(inputValue, "tail") {
		t.Fatalf("inputValue = %q, want restored queue + tail", inputValue)
	}
	if len(assistant) != 1 || !strings.Contains(assistant[0], "Restored 2 queued messages") {
		t.Fatalf("assistant output = %#v, want restore message", assistant)
	}
}

func TestExecuteSlashCommandUnknownReturnsError(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	var errText string

	_ = ExecuteSlashCommand("/missing", CommandEnv{
		Session: session,
		AppendError: func(text string) {
			errText = text
		},
	})
	if !strings.Contains(errText, "unknown slash command") {
		t.Fatalf("errText = %q, want unknown slash command", errText)
	}
}
