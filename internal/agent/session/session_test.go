package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gar/internal/llm"
	sessionstore "gar/internal/session"
)

type fakeRunner struct {
	runFn         func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error)
	captured      [][]llm.Message
	steeringCalls []llm.Message
	followCalls   []llm.Message
}

func (f *fakeRunner) Run(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	f.captured = append(f.captured, cloneMessages(req.Messages))
	if f.runFn != nil {
		return f.runFn(ctx, req)
	}
	out := make(chan llm.Event)
	close(out)
	return out, nil
}

func (f *fakeRunner) Steer(msg llm.Message) {
	f.steeringCalls = append(f.steeringCalls, msg)
}

func (f *fakeRunner) FollowUp(msg llm.Message) {
	f.followCalls = append(f.followCalls, msg)
}

func (f *fakeRunner) ClearAllQueues() {
	f.steeringCalls = nil
	f.followCalls = nil
}

func TestNewRequiresRunnerAndSessionID(t *testing.T) {
	t.Parallel()

	_, err := New(context.Background(), Config{SessionID: "s-1"})
	if !errors.Is(err, ErrRunnerRequired) {
		t.Fatalf("New() err = %v, want ErrRunnerRequired", err)
	}

	_, err = New(context.Background(), Config{Runner: &fakeRunner{}})
	if !errors.Is(err, ErrSessionIDRequired) {
		t.Fatalf("New() err = %v, want ErrSessionIDRequired", err)
	}
}

func TestSubmitAndRecordEventPersistsConversation(t *testing.T) {
	t.Parallel()

	store, err := sessionstore.NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() err = %v", err)
	}

	runner := &fakeRunner{}
	session, err := New(context.Background(), Config{
		Runner:    runner,
		Store:     store,
		SessionID: "sess-1",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	stream, err := session.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Submit() err = %v", err)
	}
	drain(stream)

	if err := session.RecordEvent(context.Background(), llm.Event{Type: llm.EventTextDelta, TextDelta: "world"}); err != nil {
		t.Fatalf("RecordEvent(text_delta) err = %v", err)
	}
	if err := session.RecordEvent(context.Background(), llm.Event{Type: llm.EventDone, Done: &llm.DonePayload{Reason: llm.StopReasonStop}}); err != nil {
		t.Fatalf("RecordEvent(done) err = %v", err)
	}

	messages := session.Messages()
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	if messages[0].Role != llm.RoleUser || messages[0].Content[0].Text != "hello" {
		t.Fatalf("message[0] = %#v, want user hello", messages[0])
	}
	if messages[1].Role != llm.RoleAssistant || messages[1].Content[0].Text != "world" {
		t.Fatalf("message[1] = %#v, want assistant world", messages[1])
	}

	entries, err := store.Load(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}
	if entries[0].Type != "user" || entries[1].Type != "assistant" {
		t.Fatalf("entries = %#v, want user then assistant", entries)
	}
}

func TestQueueDeliveryEventDequeuesAndAppendsMessage(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	session, err := New(context.Background(), Config{
		Runner:    runner,
		SessionID: "queue-1",
	})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	if err := session.QueueSteer("steer-1"); err != nil {
		t.Fatalf("QueueSteer() err = %v", err)
	}
	if err := session.QueueFollowUp("follow-1"); err != nil {
		t.Fatalf("QueueFollowUp() err = %v", err)
	}

	if got := len(session.SteeringQueued()); got != 1 {
		t.Fatalf("steering queued = %d, want 1", got)
	}
	if got := len(session.FollowUpQueued()); got != 1 {
		t.Fatalf("follow-up queued = %d, want 1", got)
	}

	if err := session.RecordEvent(context.Background(), llm.Event{
		Type: llm.EventQueuedMessage,
		Message: &llm.Message{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{{
				Type: llm.ContentTypeText,
				Text: "steer-1",
			}},
		},
	}); err != nil {
		t.Fatalf("RecordEvent(queued_message) err = %v", err)
	}

	if got := len(session.SteeringQueued()); got != 0 {
		t.Fatalf("steering queued = %d, want 0", got)
	}
	if got := len(session.FollowUpQueued()); got != 1 {
		t.Fatalf("follow-up queued = %d, want 1", got)
	}

	messages := session.Messages()
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(messages))
	}
	if messages[0].Role != llm.RoleUser || messages[0].Content[0].Text != "steer-1" {
		t.Fatalf("message = %#v, want queued steer user message", messages[0])
	}
}

func TestSwitchBranchCreatesDivergentTree(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	session, err := New(context.Background(), Config{
		Runner:    runner,
		SessionID: "tree-1",
	})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	stream, err := session.Submit(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Submit(u1) err = %v", err)
	}
	drain(stream)
	if err := session.RecordEvent(context.Background(), llm.Event{Type: llm.EventTextDelta, TextDelta: "a1"}); err != nil {
		t.Fatalf("RecordEvent(a1 delta) err = %v", err)
	}
	if err := session.RecordEvent(context.Background(), llm.Event{Type: llm.EventDone, Done: &llm.DonePayload{Reason: llm.StopReasonStop}}); err != nil {
		t.Fatalf("RecordEvent(a1 done) err = %v", err)
	}

	stream, err = session.Submit(context.Background(), "u2")
	if err != nil {
		t.Fatalf("Submit(u2) err = %v", err)
	}
	drain(stream)

	if err := session.SwitchBranch("000001"); err != nil {
		t.Fatalf("SwitchBranch(000001) err = %v", err)
	}
	stream, err = session.Submit(context.Background(), "u1-branch")
	if err != nil {
		t.Fatalf("Submit(u1-branch) err = %v", err)
	}
	drain(stream)

	if got := session.LeafID(); got != "000004" {
		t.Fatalf("LeafID() = %s, want 000004", got)
	}

	lines := session.TreeLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "000002") || !strings.Contains(joined, "000004") {
		t.Fatalf("tree lines missing branches:\n%s", joined)
	}
}

func TestCompactAddsSummaryAndKeepsTail(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	session, err := New(context.Background(), Config{
		Runner:         runner,
		SessionID:      "compact-1",
		CompactionKeep: 2,
	})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	for i := 1; i <= 3; i++ {
		stream, err := session.Submit(context.Background(), "user")
		if err != nil {
			t.Fatalf("Submit(%d) err = %v", i, err)
		}
		drain(stream)
		if err := session.RecordEvent(context.Background(), llm.Event{Type: llm.EventTextDelta, TextDelta: "assistant"}); err != nil {
			t.Fatalf("RecordEvent(delta %d) err = %v", i, err)
		}
		if err := session.RecordEvent(context.Background(), llm.Event{Type: llm.EventDone, Done: &llm.DonePayload{Reason: llm.StopReasonStop}}); err != nil {
			t.Fatalf("RecordEvent(done %d) err = %v", i, err)
		}
	}

	result, err := session.Compact(context.Background(), 2, "")
	if err != nil {
		t.Fatalf("Compact() err = %v", err)
	}
	if result.DroppedMessages <= 0 {
		t.Fatalf("DroppedMessages = %d, want > 0", result.DroppedMessages)
	}

	messages := session.Messages()
	if len(messages) < 3 {
		t.Fatalf("messages len = %d, want at least 3 (summary + kept tail)", len(messages))
	}
	if messages[0].Role != llm.RoleAssistant {
		t.Fatalf("messages[0].Role = %s, want assistant summary", messages[0].Role)
	}
	if !strings.Contains(messages[0].Content[0].Text, "Context Compact Summary") {
		t.Fatalf("summary = %q, want Context Compact Summary", messages[0].Content[0].Text)
	}

	entries := session.Entries()
	foundCompaction := false
	for _, entry := range entries {
		if entry.Type == "compaction" {
			foundCompaction = true
			break
		}
	}
	if !foundCompaction {
		t.Fatalf("expected compaction entry in session entries")
	}
}

func TestSessionManagementNewSwitchAndName(t *testing.T) {
	t.Parallel()

	store, err := sessionstore.NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() err = %v", err)
	}

	runner := &fakeRunner{}
	session, err := New(context.Background(), Config{
		Runner:    runner,
		Store:     store,
		SessionID: "sess-a",
		Meta:      map[string]any{"model": "claude"},
	})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	stream, err := session.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Submit() err = %v", err)
	}
	drain(stream)

	if err := session.SetSessionName(context.Background(), "alpha"); err != nil {
		t.Fatalf("SetSessionName() err = %v", err)
	}
	if got := session.SessionName(); got != "alpha" {
		t.Fatalf("SessionName() = %q, want alpha", got)
	}
	if got := session.Stats().SessionName; got != "alpha" {
		t.Fatalf("Stats().SessionName = %q, want alpha", got)
	}

	listed, err := session.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() err = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "sess-a" {
		t.Fatalf("ListSessions() = %#v, want sess-a", listed)
	}

	newID, err := session.NewSession(context.Background(), "sess-b")
	if err != nil {
		t.Fatalf("NewSession() err = %v", err)
	}
	if newID != "sess-b" {
		t.Fatalf("NewSession() id = %q, want sess-b", newID)
	}
	if got := session.SessionID(); got != "sess-b" {
		t.Fatalf("SessionID() = %q, want sess-b", got)
	}
	if len(session.Messages()) != 0 {
		t.Fatalf("Messages() should be empty on new session")
	}
	if got := session.SessionName(); got != "" {
		t.Fatalf("SessionName() = %q, want empty after NewSession", got)
	}

	if err := session.SwitchSession(context.Background(), "sess-a"); err != nil {
		t.Fatalf("SwitchSession(sess-a) err = %v", err)
	}
	if got := session.SessionID(); got != "sess-a" {
		t.Fatalf("SessionID() after switch = %q, want sess-a", got)
	}
	if got := session.SessionName(); got != "alpha" {
		t.Fatalf("SessionName() after switch = %q, want alpha", got)
	}
	messages := session.Messages()
	if len(messages) != 1 || messages[0].Role != llm.RoleUser || messages[0].Content[0].Text != "hello" {
		t.Fatalf("Messages() after switch = %#v, want persisted user hello", messages)
	}
}

func TestListSessionsRequiresStore(t *testing.T) {
	t.Parallel()

	session, err := New(context.Background(), Config{
		Runner:    &fakeRunner{},
		SessionID: "ephemeral-1",
	})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	if _, err := session.ListSessions(context.Background()); !errors.Is(err, ErrSessionStoreRequired) {
		t.Fatalf("ListSessions() err = %v, want ErrSessionStoreRequired", err)
	}
	if err := session.SwitchSession(context.Background(), "x"); !errors.Is(err, ErrSessionStoreRequired) {
		t.Fatalf("SwitchSession() err = %v, want ErrSessionStoreRequired", err)
	}
}

func drain(stream <-chan llm.Event) {
	if stream == nil {
		return
	}
	for range stream {
	}
}
