package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gar/internal/llm"
	sessionstore "gar/internal/session"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeRunner struct {
	calls    int
	captured []*llm.Request
	streamFn func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error)
	steering []llm.Message
	followUp []llm.Message
}

func (r *fakeRunner) Run(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	r.calls++
	r.captured = append(r.captured, req)
	return r.streamFn(ctx, req)
}

func (r *fakeRunner) Steer(msg llm.Message) {
	r.steering = append(r.steering, msg)
}

func (r *fakeRunner) FollowUp(msg llm.Message) {
	r.followUp = append(r.followUp, msg)
}

func (r *fakeRunner) ClearAllQueues() {
	r.steering = nil
	r.followUp = nil
}

func TestInputModelHandleKey(t *testing.T) {
	t.Parallel()

	input := NewInputModel(">", "placeholder")
	if submitted := input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")}); submitted {
		t.Fatalf("unexpected submit on rune key")
	}
	if submitted := input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")}); submitted {
		t.Fatalf("unexpected submit on rune key")
	}
	if got := input.Value(); got != "hi" {
		t.Fatalf("input value = %q, want hi", got)
	}

	if submitted := input.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace}); submitted {
		t.Fatalf("unexpected submit on backspace")
	}
	if got := input.Value(); got != "h" {
		t.Fatalf("input value after backspace = %q, want h", got)
	}

	if submitted := input.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}); !submitted {
		t.Fatalf("expected submit on enter")
	}
}

func TestAppFlushesAssistantOnDoneEvent(t *testing.T) {
	t.Parallel()

	app := NewApp(AppConfig{ShowInspector: true})

	_, _ = app.Update(StreamEventMsg{Event: llm.Event{
		Type: llm.EventContentBlockStart,
		ContentBlockStart: &llm.ContentBlockStart{
			Type: "text",
			Text: "hello",
		},
	}})
	_, _ = app.Update(StreamEventMsg{Event: llm.Event{Type: llm.EventTextDelta, TextDelta: " world"}})
	_, _ = app.Update(StreamEventMsg{Event: llm.Event{Type: llm.EventDone, Done: &llm.DonePayload{Reason: llm.StopReasonStop}}})

	messages := app.chat.Messages()
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if messages[0].Role != "assistant" || messages[0].Content != "hello world" {
		t.Fatalf("assistant message = %#v, want role=assistant content=hello world", messages[0])
	}
	if got := app.status.State; got != "idle" {
		t.Fatalf("status state = %q, want idle", got)
	}
	if got := app.inspector.State; got != "idle" {
		t.Fatalf("inspector state = %q, want idle", got)
	}
}

func TestAppTracksToolCallInInspector(t *testing.T) {
	t.Parallel()

	app := NewApp(AppConfig{ShowInspector: true})

	_, _ = app.Update(StreamEventMsg{Event: llm.Event{
		Type: llm.EventToolCallStart,
		ToolCall: &llm.ToolCall{
			Name: "read",
		},
	}})

	if got := app.inspector.ToolCounts["read"]; got != 1 {
		t.Fatalf("tool count = %d, want 1", got)
	}
	if got := app.status.State; got != "tool_executing" {
		t.Fatalf("status state = %q, want tool_executing", got)
	}
}

func TestAppSubmitRunsRunnerAndRendersAssistantReply(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			out := make(chan llm.Event, 2)
			out <- llm.Event{
				Type: llm.EventContentBlockStart,
				ContentBlockStart: &llm.ContentBlockStart{
					Type: "text",
					Text: "hello",
				},
			}
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{Reason: llm.StopReasonStop},
			}
			close(out)
			return out, nil
		},
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		Runner:        runner,
		MaxTokens:     64,
	})

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for cmd != nil {
		msg := cmd()
		_, cmd = app.Update(msg)
	}

	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if len(runner.captured) != 1 {
		t.Fatalf("captured request count = %d, want 1", len(runner.captured))
	}
	req := runner.captured[0]
	if req.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("request model = %q", req.Model)
	}
	if req.MaxTokens != 64 {
		t.Fatalf("request max_tokens = %d, want 64", req.MaxTokens)
	}
	if len(req.Messages) == 0 {
		t.Fatalf("request messages empty")
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llm.RoleUser || len(last.Content) == 0 || last.Content[0].Text != "hi" {
		t.Fatalf("last request message = %#v, want user hi", last)
	}

	messages := app.chat.Messages()
	if len(messages) != 2 {
		t.Fatalf("chat messages = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hi" {
		t.Fatalf("first chat message = %#v, want user hi", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "hello" {
		t.Fatalf("second chat message = %#v, want assistant hello", messages[1])
	}
	if got := app.status.State; got != "idle" {
		t.Fatalf("status state = %q, want idle", got)
	}
}

func TestAppSubmitShowsRunError(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			return nil, llm.ErrMissingAPIKey
		},
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		Runner:        runner,
		MaxTokens:     64,
	})

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		msg := cmd()
		_, _ = app.Update(msg)
	}

	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if got := app.status.State; got != "error" {
		t.Fatalf("status state = %q, want error", got)
	}

	messages := app.chat.Messages()
	if len(messages) != 2 {
		t.Fatalf("chat messages = %d, want 2", len(messages))
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("last role = %q, want assistant", last.Role)
	}
	if !strings.Contains(last.Content, llm.ErrMissingAPIKey.Error()) {
		t.Fatalf("last content = %q, want missing api key", last.Content)
	}
}

func TestAppSubmitWhileBusyQueuesSteeringMessage(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event)
			go func() {
				defer close(out)
				<-block
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{Reason: llm.StopReasonStop},
				}
			}()
			return out, nil
		},
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		Runner:        runner,
		MaxTokens:     64,
	})

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected stream command")
	}

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	close(block)

	msg := cmd()
	_, cmd = app.Update(msg)
	for cmd != nil {
		msg = cmd()
		_, cmd = app.Update(msg)
	}

	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	messages := app.chat.Messages()
	if len(messages) < 2 {
		t.Fatalf("chat messages = %d, want at least 2", len(messages))
	}
	foundQueued := false
	for _, message := range messages {
		if message.Role == "assistant" && strings.Contains(message.Content, "Queued steer message.") {
			foundQueued = true
			break
		}
	}
	if !foundQueued {
		t.Fatalf("expected queued steer message in chat, messages=%#v", messages)
	}
}

func TestAppConsumeEventErrorWithoutErrorValue(t *testing.T) {
	t.Parallel()

	app := NewApp(AppConfig{ShowInspector: true})
	_, _ = app.Update(StreamEventMsg{Event: llm.Event{
		Type: llm.EventError,
		Done: &llm.DonePayload{Reason: llm.StopReasonError},
		Err:  errors.New("boom"),
	}})

	messages := app.chat.Messages()
	if len(messages) != 1 {
		t.Fatalf("chat messages = %d, want 1", len(messages))
	}
	if messages[0].Role != "assistant" || !strings.Contains(messages[0].Content, "boom") {
		t.Fatalf("error message = %#v, want assistant boom", messages[0])
	}
}

func TestAppDoneToolUseDoesNotTerminateActiveStream(t *testing.T) {
	t.Parallel()

	app := NewApp(AppConfig{ShowInspector: true})
	stream := make(chan llm.Event)
	app.activeStream = stream
	app.status.SetState("streaming")
	app.inspector.SetState("streaming")

	_, cmd := app.Update(StreamEventMsg{Event: llm.Event{
		Type: llm.EventDone,
		Done: &llm.DonePayload{Reason: llm.StopReasonToolUse},
	}})

	if app.activeStream == nil {
		t.Fatalf("activeStream cleared on tool_use done; want stream to remain active")
	}
	if app.status.State == "idle" {
		t.Fatalf("status state = %q, want non-idle during tool_use continuation", app.status.State)
	}
	if app.inspector.State == "idle" {
		t.Fatalf("inspector state = %q, want non-idle during tool_use continuation", app.inspector.State)
	}
	if cmd == nil {
		t.Fatalf("expected readStreamEventCommand to continue reading active stream")
	}
}

func TestAppArrowKeysScrollChat(t *testing.T) {
	t.Parallel()

	app := NewApp(AppConfig{ShowInspector: false})
	_, _ = app.Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	for i := 1; i <= 8; i++ {
		app.chat.Append("user", fmt.Sprintf("line-%d", i))
	}

	_ = app.View() // primes viewport sizing
	initialTop := app.chat.scrollTop
	if initialTop == 0 {
		t.Fatalf("expected initial scrollTop > 0 with overflowing chat, got %d", initialTop)
	}

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyUp})
	if app.chat.scrollTop != initialTop-1 {
		t.Fatalf("scrollTop after up = %d, want %d", app.chat.scrollTop, initialTop-1)
	}

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if app.chat.scrollTop != initialTop {
		t.Fatalf("scrollTop after down = %d, want %d", app.chat.scrollTop, initialTop)
	}
}

func TestAppSlashHelpShowsCommands(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event)
			close(out)
			return out, nil
		},
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		Runner:        runner,
		MaxTokens:     64,
	})

	for _, r := range []rune("/help") {
		_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	messages := app.chat.Messages()
	if len(messages) == 0 {
		t.Fatalf("expected slash help message")
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content, "Slash commands:") {
		t.Fatalf("last message = %#v, want slash help", last)
	}
}

func TestAppSlashQueueShowsQueuedMessages(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event)
			go func() {
				defer close(out)
				<-block
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{Reason: llm.StopReasonStop},
				}
			}()
			return out, nil
		},
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		Runner:        runner,
		MaxTokens:     64,
	})

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected stream command")
	}

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	for _, r := range []rune("/queue") {
		_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	messages := app.chat.Messages()
	foundQueue := false
	for _, message := range messages {
		if message.Role == "assistant" && strings.Contains(message.Content, "- steer: b") {
			foundQueue = true
			break
		}
	}
	if !foundQueue {
		t.Fatalf("expected /queue output with steer message, messages=%#v", messages)
	}
	if len(runner.steering) != 1 {
		t.Fatalf("steering calls = %d, want 1", len(runner.steering))
	}

	close(block)
	msg := cmd()
	_, cmd = app.Update(msg)
	for cmd != nil {
		msg = cmd()
		_, cmd = app.Update(msg)
	}
}

func TestAppSlashNameAndSession(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event)
			close(out)
			return out, nil
		},
	}

	store, err := sessionstore.NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() err = %v", err)
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		SessionID:     "named-session",
		Runner:        runner,
		MaxTokens:     64,
		SessionStore:  store,
	})

	for _, text := range []string{"/name alpha", "/session"} {
		for _, r := range []rune(text) {
			_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}

	messages := app.chat.Messages()
	found := false
	for _, message := range messages {
		if message.Role == "assistant" && strings.Contains(message.Content, `name="alpha"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /session output to include name alpha, messages=%#v", messages)
	}
}

func TestAppSlashNewAndResume(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			out := make(chan llm.Event, 1)
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{Reason: llm.StopReasonStop},
			}
			close(out)
			return out, nil
		},
	}

	store, err := sessionstore.NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() err = %v", err)
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		SessionID:     "resume-a",
		Runner:        runner,
		MaxTokens:     64,
		SessionStore:  store,
	})

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for cmd != nil {
		msg := cmd()
		_, cmd = app.Update(msg)
	}

	for _, text := range []string{"/new", "/resume resume-a"} {
		for _, r := range []rune(text) {
			_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}

	if got := app.status.SessionID; got != "resume-a" {
		t.Fatalf("status.SessionID = %q, want resume-a", got)
	}

	messages := app.chat.Messages()
	found := false
	for _, message := range messages {
		if message.Role == "assistant" && strings.Contains(message.Content, "Resumed session resume-a.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected resumed message, messages=%#v", messages)
	}
}

func TestAppResumeSelectorKeyboardSwitchesSession(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event, 1)
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{Reason: llm.StopReasonStop},
			}
			close(out)
			return out, nil
		},
	}

	store, err := sessionstore.NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() err = %v", err)
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		SessionID:     "s1",
		Runner:        runner,
		MaxTokens:     64,
		SessionStore:  store,
	})

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for cmd != nil {
		msg := cmd()
		_, cmd = app.Update(msg)
	}

	for _, text := range []string{"/new", "/resume"} {
		for _, r := range []rune(text) {
			_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}

	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := app.status.SessionID; got != "s1" {
		t.Fatalf("status.SessionID = %q, want s1 after selector confirm", got)
	}
}

func TestAppTreeSelectorKeyboardSwitchesLeaf(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event, 1)
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{Reason: llm.StopReasonStop},
			}
			close(out)
			return out, nil
		},
	}

	app := NewApp(AppConfig{
		ShowInspector: true,
		ModelName:     "claude-sonnet-4-20250514",
		SessionID:     "tree-selector",
		Runner:        runner,
		MaxTokens:     64,
	})

	for _, text := range []string{"u1", "u2"} {
		for _, r := range []rune(text) {
			_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		for cmd != nil {
			msg := cmd()
			_, cmd = app.Update(msg)
		}
	}

	for _, text := range []string{"/branch 000001", "u1b"} {
		for _, r := range []rune(text) {
			_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		for cmd != nil {
			msg := cmd()
			_, cmd = app.Update(msg)
		}
	}

	if got := app.session.LeafID(); got != "000004" {
		t.Fatalf("precondition leaf = %q, want 000004", got)
	}

	for _, r := range []rune("/tree") {
		_, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyUp})
	_, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := app.session.LeafID(); got != "000003" {
		t.Fatalf("leaf after tree selector = %q, want 000003", got)
	}
}
