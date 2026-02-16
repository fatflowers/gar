package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"gar/internal/llm"
	"gar/internal/tools"
)

type fakeProvider struct {
	streamFn func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error)
}

func (p fakeProvider) Stream(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	return p.streamFn(ctx, req)
}

type fakeTool struct {
	name string
	run  func(ctx context.Context, params json.RawMessage) (tools.Result, error)
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) Description() string { return "fake tool" }

func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (f fakeTool) Execute(ctx context.Context, params json.RawMessage) (tools.Result, error) {
	if f.run == nil {
		return tools.Result{}, nil
	}
	return f.run(ctx, params)
}

func TestNewRequiresProvider(t *testing.T) {
	t.Parallel()

	_, err := New(Config{})
	if !errors.Is(err, ErrProviderRequired) {
		t.Fatalf("expected ErrProviderRequired, got %v", err)
	}
}

func TestRunStateTransitionsAndBackToIdle(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			out := make(chan llm.Event)
			go func() {
				defer close(out)
				close(started)
				out <- llm.Event{Type: llm.EventStart}
				<-release
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{
						Reason: llm.StopReasonStop,
					},
				}
			}()
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentTypeText, Text: "hi"}}},
		},
		MaxTokens: 128,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatalf("provider did not start in time")
	}

	if got := a.State(); got != StateStreaming {
		t.Fatalf("State() = %s, want %s", got, StateStreaming)
	}

	close(release)

	var gotEvents []llm.EventType
	for ev := range stream {
		gotEvents = append(gotEvents, ev.Type)
	}
	if len(gotEvents) != 2 {
		t.Fatalf("expected 2 events, got %d", len(gotEvents))
	}
	if gotEvents[0] != llm.EventStart || gotEvents[1] != llm.EventDone {
		t.Fatalf("unexpected events: %#v", gotEvents)
	}

	eventually(t, 1*time.Second, func() bool {
		return a.State() == StateIdle
	})
}

func TestRunReturnsBusyWhenAlreadyRunning(t *testing.T) {
	t.Parallel()

	var once sync.Once
	release := make(chan struct{})

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			out := make(chan llm.Event)
			go func() {
				defer close(out)
				once.Do(func() {
					out <- llm.Event{Type: llm.EventStart}
					<-release
				})
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{Reason: llm.StopReasonStop},
				}
			}()
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	first, err := a.Run(context.Background(), &llm.Request{Model: "claude-sonnet-4-20250514", MaxTokens: 32})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	_, err = a.Run(context.Background(), &llm.Request{Model: "claude-sonnet-4-20250514", MaxTokens: 32})
	if !errors.Is(err, ErrAgentBusy) {
		t.Fatalf("expected ErrAgentBusy, got %v", err)
	}

	close(release)
	for range first {
	}
}

func TestCancelStopsAgent(t *testing.T) {
	t.Parallel()

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			out := make(chan llm.Event)
			go func() {
				defer close(out)
				out <- llm.Event{Type: llm.EventStart}
				<-ctx.Done()
				out <- llm.Event{
					Type: llm.EventError,
					Done: &llm.DonePayload{Reason: llm.StopReasonAborted},
					Err:  ctx.Err(),
				}
			}()
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{Model: "claude-sonnet-4-20250514", MaxTokens: 32})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var seenStart bool
	var seenAbort bool
	for ev := range stream {
		if ev.Type == llm.EventStart && !seenStart {
			seenStart = true
			a.Cancel()
			continue
		}
		if ev.Type == llm.EventError && ev.Done != nil && ev.Done.Reason == llm.StopReasonAborted {
			seenAbort = true
		}
	}

	if !seenStart {
		t.Fatalf("expected start event")
	}
	if !seenAbort {
		t.Fatalf("expected aborted error event")
	}
	if got := a.State(); got != StateIdle {
		t.Fatalf("State() = %s, want %s", got, StateIdle)
	}
}

func TestRunReturnsToIdleWhenTerminalEventCannotBeDelivered(t *testing.T) {
	t.Parallel()

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			out := make(chan llm.Event)
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_ = stream // Intentionally abandon the stream to verify cleanup still happens.

	eventually(t, 1*time.Second, func() bool {
		return a.State() == StateIdle
	})
}

func TestRunReturnsToIdleWhenCallerAbandonsMultiEventStream(t *testing.T) {
	t.Parallel()

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event, 2)
			out <- llm.Event{Type: llm.EventStart}
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{
					Reason: llm.StopReasonStop,
				},
			}
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_ = stream // Intentionally abandon the stream after Run() starts.
	defer a.Cancel()

	eventually(t, 1*time.Second, func() bool {
		return a.State() == StateIdle
	})
}

func TestStateTransitionsToErrorOnProviderTerminalProtocolFailure(t *testing.T) {
	t.Parallel()

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req

			out := make(chan llm.Event, 1)
			out <- llm.Event{Type: llm.EventStart}
			close(out) // Missing done/error terminal event to trigger protocol failure path.
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_ = stream // Keep output undrained so error state is observable before cleanup.

	eventually(t, 1*time.Second, func() bool {
		return a.State() == StateError
	})

	eventually(t, 1*time.Second, func() bool {
		return a.State() == StateIdle
	})
}

func TestContinueRequiresExistingMessages(t *testing.T) {
	t.Parallel()

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event)
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = a.Continue(context.Background(), &llm.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 32,
	})
	if !errors.Is(err, ErrNoMessagesToContinue) {
		t.Fatalf("expected ErrNoMessagesToContinue, got %v", err)
	}
}

func TestContinueAssistantTailNeedsQueuedMessages(t *testing.T) {
	t.Parallel()

	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			out := make(chan llm.Event)
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = a.Continue(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleAssistant,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "done"},
				},
			},
		},
		MaxTokens: 32,
	})
	if !errors.Is(err, ErrContinueFromAssistantTail) {
		t.Fatalf("expected ErrContinueFromAssistantTail, got %v", err)
	}
}

func TestContinueWithQueuedFollowUpRuns(t *testing.T) {
	t.Parallel()

	var streamCalls int
	var captured []llm.Message
	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			streamCalls++
			captured = cloneMessagesForTest(req.Messages)

			out := make(chan llm.Event, 2)
			out <- llm.Event{Type: llm.EventStart}
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{
					Reason: llm.StopReasonStop,
				},
			}
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	a.FollowUp(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "queued follow-up"},
		},
	})

	stream, err := a.Continue(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleAssistant,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "initial reply"},
				},
			},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Continue() error = %v", err)
	}
	for range stream {
	}

	if got := lastUserText(captured); got != "queued follow-up" {
		t.Fatalf("last user message = %q, want queued follow-up", got)
	}
	if streamCalls != 1 {
		t.Fatalf("provider stream calls = %d, want 1", streamCalls)
	}
}

func TestRunQueuedMessagesSteeringBeforeFollowUp(t *testing.T) {
	t.Parallel()

	var snapshots [][]llm.Message
	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			snapshots = append(snapshots, cloneMessagesForTest(req.Messages))

			out := make(chan llm.Event, 2)
			out <- llm.Event{Type: llm.EventStart}
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{
					Reason: llm.StopReasonStop,
				},
			}
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	a.Steer(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "steer-1"},
		},
	})
	a.Steer(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "steer-2"},
		},
	})
	a.FollowUp(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "follow-1"},
		},
	})

	req := &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "initial"},
				},
			},
		},
		MaxTokens: 32,
	}
	stream, err := a.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for range stream {
	}

	if len(req.Messages) != 1 {
		t.Fatalf("Run() mutated input request messages: got %d want 1", len(req.Messages))
	}

	if len(snapshots) != 3 {
		t.Fatalf("provider call count = %d, want 3", len(snapshots))
	}

	if got := lastUserText(snapshots[0]); got != "steer-1" {
		t.Fatalf("turn 1 last user = %q, want steer-1", got)
	}
	if got := lastUserText(snapshots[1]); got != "steer-2" {
		t.Fatalf("turn 2 last user = %q, want steer-2", got)
	}
	if got := lastUserText(snapshots[2]); got != "follow-1" {
		t.Fatalf("turn 3 last user = %q, want follow-1", got)
	}
}

func TestRunQueuedMessagesAllMode(t *testing.T) {
	t.Parallel()

	var snapshots [][]llm.Message
	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			snapshots = append(snapshots, cloneMessagesForTest(req.Messages))

			out := make(chan llm.Event, 2)
			out <- llm.Event{Type: llm.EventStart}
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{
					Reason: llm.StopReasonStop,
				},
			}
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{
		Provider:     provider,
		SteeringMode: QueueModeAll,
		FollowUpMode: QueueModeAll,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	a.Steer(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "steer-1"},
		},
	})
	a.Steer(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "steer-2"},
		},
	})
	a.FollowUp(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "follow-1"},
		},
	})
	a.FollowUp(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "follow-2"},
		},
	})

	stream, err := a.Run(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "initial"},
				},
			},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for range stream {
	}

	if len(snapshots) != 2 {
		t.Fatalf("provider call count = %d, want 2", len(snapshots))
	}

	if got := userTexts(snapshots[0]); len(got) != 3 || got[1] != "steer-1" || got[2] != "steer-2" {
		t.Fatalf("turn 1 user texts = %#v, want [initial steer-1 steer-2]", got)
	}
	if got := userTexts(snapshots[1]); len(got) != 5 || got[3] != "follow-1" || got[4] != "follow-2" {
		t.Fatalf("turn 2 user texts = %#v, want ... follow-1 follow-2", got)
	}
}

func TestRunStopsAfterToolUseWithoutRetryLoop(t *testing.T) {
	t.Parallel()

	var streamCalls int
	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			streamCalls++

			out := make(chan llm.Event, 3)
			out <- llm.Event{Type: llm.EventStart}
			out <- llm.Event{
				Type: llm.EventToolCallEnd,
				ToolCall: &llm.ToolCall{
					ID:        "call-1",
					Name:      "read_file",
					Arguments: []byte(`{"path":"main.go"}`),
				},
			}
			out <- llm.Event{
				Type: llm.EventDone,
				Done: &llm.DonePayload{
					Reason: llm.StopReasonToolUse,
				},
			}
			close(out)
			return out, nil
		},
	}

	a, err := New(Config{Provider: provider, MaxTurns: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "list files"},
				},
			},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var sawToolUseDone bool
	for ev := range stream {
		if ev.Type == llm.EventDone && ev.Done != nil && ev.Done.Reason == llm.StopReasonToolUse {
			sawToolUseDone = true
		}
		if ev.Type == llm.EventError {
			t.Fatalf("unexpected error event: %v", ev.Err)
		}
	}

	if streamCalls != 1 {
		t.Fatalf("provider stream calls = %d, want 1", streamCalls)
	}
	if !sawToolUseDone {
		t.Fatalf("expected tool_use done event")
	}
}

func TestRunExecutesToolUseAndContinues(t *testing.T) {
	t.Parallel()

	var streamCalls int
	var snapshots [][]llm.Message
	var sawToolResultEvent bool
	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			streamCalls++
			snapshots = append(snapshots, cloneMessagesForTest(req.Messages))

			out := make(chan llm.Event, 3)
			out <- llm.Event{Type: llm.EventStart}
			if streamCalls == 1 {
				out <- llm.Event{
					Type: llm.EventToolCallEnd,
					ToolCall: &llm.ToolCall{
						ID:        "call-1",
						Name:      "echo",
						Arguments: json.RawMessage(`{"value":"hello"}`),
					},
				}
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{
						Reason: llm.StopReasonToolUse,
					},
				}
			} else {
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{
						Reason: llm.StopReasonStop,
					},
				}
			}
			close(out)
			return out, nil
		},
	}

	registry := tools.NewRegistry()
	if err := registry.Register(fakeTool{
		name: "echo",
		run: func(ctx context.Context, params json.RawMessage) (tools.Result, error) {
			_ = ctx
			return tools.Result{Content: `{"echo":"ok"}`}, nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	a, err := New(Config{
		Provider:     provider,
		MaxTurns:     5,
		ToolRegistry: registry,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "run tool"},
				},
			},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for ev := range stream {
		if ev.Type == llm.EventToolResult && ev.ToolResult != nil && ev.ToolResult.ToolCallID == "call-1" {
			sawToolResultEvent = true
		}
	}

	if streamCalls != 2 {
		t.Fatalf("provider stream calls = %d, want 2", streamCalls)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(snapshots))
	}

	second := snapshots[1]
	if len(second) == 0 {
		t.Fatalf("second turn has no messages")
	}
	last := second[len(second)-1]
	if last.Role != llm.RoleTool || last.ToolResult == nil {
		t.Fatalf("last second-turn message = %#v, want tool_result message", last)
	}
	if last.ToolResult.ToolCallID != "call-1" {
		t.Fatalf("tool_call_id = %q, want call-1", last.ToolResult.ToolCallID)
	}
	if last.ToolResult.ToolName != "echo" {
		t.Fatalf("tool_name = %q, want echo", last.ToolResult.ToolName)
	}
	if last.ToolResult.Content != `{"echo":"ok"}` {
		t.Fatalf("tool_result content = %q, want {\"echo\":\"ok\"}", last.ToolResult.Content)
	}
	if last.ToolResult.IsError {
		t.Fatalf("tool_result IsError = true, want false")
	}
	if !sawToolResultEvent {
		t.Fatalf("expected EventToolResult in stream")
	}
}

func TestRunSkipsRemainingToolCallsWhenSteeringQueuedAfterTool(t *testing.T) {
	t.Parallel()

	var streamCalls int
	var snapshots [][]llm.Message
	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			streamCalls++
			snapshots = append(snapshots, cloneMessagesForTest(req.Messages))

			out := make(chan llm.Event, 4)
			out <- llm.Event{Type: llm.EventStart}
			if streamCalls == 1 {
				out <- llm.Event{
					Type: llm.EventToolCallEnd,
					ToolCall: &llm.ToolCall{
						ID:        "call-1",
						Name:      "first",
						Arguments: json.RawMessage(`{}`),
					},
				}
				out <- llm.Event{
					Type: llm.EventToolCallEnd,
					ToolCall: &llm.ToolCall{
						ID:        "call-2",
						Name:      "second",
						Arguments: json.RawMessage(`{}`),
					},
				}
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{
						Reason: llm.StopReasonToolUse,
					},
				}
			} else {
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{
						Reason: llm.StopReasonStop,
					},
				}
			}
			close(out)
			return out, nil
		},
	}

	started := make(chan struct{})
	release := make(chan struct{})
	secondRan := false
	registry := tools.NewRegistry()
	if err := registry.Register(fakeTool{
		name: "first",
		run: func(ctx context.Context, params json.RawMessage) (tools.Result, error) {
			_ = ctx
			_ = params
			close(started)
			<-release
			return tools.Result{Content: "first-ok"}, nil
		},
	}); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(fakeTool{
		name: "second",
		run: func(ctx context.Context, params json.RawMessage) (tools.Result, error) {
			_ = ctx
			_ = params
			secondRan = true
			return tools.Result{Content: "second-ok"}, nil
		},
	}); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	a, err := New(Config{
		Provider:     provider,
		MaxTurns:     5,
		ToolRegistry: registry,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "run tools"},
				},
			},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatalf("first tool did not start in time")
	}

	a.Steer(llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "interrupt"},
		},
	})
	close(release)

	var sawSkippedResult bool
	for ev := range stream {
		if ev.Type == llm.EventToolResult && ev.ToolResult != nil && ev.ToolResult.ToolCallID == "call-2" {
			if ev.ToolResult.Content == "Skipped due to queued user message." && ev.ToolResult.IsError {
				sawSkippedResult = true
			}
		}
	}

	if streamCalls != 2 {
		t.Fatalf("provider stream calls = %d, want 2", streamCalls)
	}
	if secondRan {
		t.Fatalf("second tool executed, want skipped")
	}
	if !sawSkippedResult {
		t.Fatalf("expected skipped tool_result for call-2")
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(snapshots))
	}

	secondTurn := snapshots[1]
	var firstResult *llm.ToolResult
	var secondResult *llm.ToolResult
	for i := range secondTurn {
		msg := secondTurn[i]
		if msg.Role != llm.RoleTool || msg.ToolResult == nil {
			continue
		}
		switch msg.ToolResult.ToolCallID {
		case "call-1":
			result := *msg.ToolResult
			firstResult = &result
		case "call-2":
			result := *msg.ToolResult
			secondResult = &result
		}
	}

	if firstResult == nil {
		t.Fatalf("missing call-1 tool_result in next turn context")
	}
	if firstResult.IsError || firstResult.Content != "first-ok" {
		t.Fatalf("call-1 tool_result = %#v, want success first-ok", firstResult)
	}
	if secondResult == nil {
		t.Fatalf("missing call-2 tool_result in next turn context")
	}
	if !secondResult.IsError || secondResult.Content != "Skipped due to queued user message." {
		t.Fatalf("call-2 tool_result = %#v, want skipped error result", secondResult)
	}
	if got := lastUserText(secondTurn); got != "interrupt" {
		t.Fatalf("second-turn last user = %q, want interrupt", got)
	}
}

func TestStateTransitionsToToolExecutingDuringToolCall(t *testing.T) {
	t.Parallel()

	var streamCalls int
	provider := fakeProvider{
		streamFn: func(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
			_ = ctx
			_ = req
			streamCalls++

			out := make(chan llm.Event, 3)
			out <- llm.Event{Type: llm.EventStart}
			if streamCalls == 1 {
				out <- llm.Event{
					Type: llm.EventToolCallEnd,
					ToolCall: &llm.ToolCall{
						ID:        "call-1",
						Name:      "slow",
						Arguments: json.RawMessage(`{}`),
					},
				}
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{
						Reason: llm.StopReasonToolUse,
					},
				}
			} else {
				out <- llm.Event{
					Type: llm.EventDone,
					Done: &llm.DonePayload{
						Reason: llm.StopReasonStop,
					},
				}
			}
			close(out)
			return out, nil
		},
	}

	started := make(chan struct{})
	release := make(chan struct{})

	registry := tools.NewRegistry()
	if err := registry.Register(fakeTool{
		name: "slow",
		run: func(ctx context.Context, params json.RawMessage) (tools.Result, error) {
			_ = ctx
			_ = params
			close(started)
			<-release
			return tools.Result{Content: "ok"}, nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	a, err := New(Config{
		Provider:     provider,
		MaxTurns:     5,
		ToolRegistry: registry,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := a.Run(context.Background(), &llm.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "run slow tool"},
				},
			},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatalf("tool did not start in time")
	}

	eventually(t, 1*time.Second, func() bool {
		return a.State() == StateToolExecuting
	})

	close(release)
	for range stream {
	}

	eventually(t, 1*time.Second, func() bool {
		return a.State() == StateIdle
	})
}

func cloneMessagesForTest(messages []llm.Message) []llm.Message {
	cloned := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		copyMsg := llm.Message{
			Role:      msg.Role,
			Content:   append([]llm.ContentBlock(nil), msg.Content...),
			ToolCalls: append([]llm.ToolCall(nil), msg.ToolCalls...),
		}
		if msg.ToolResult != nil {
			toolResult := *msg.ToolResult
			copyMsg.ToolResult = &toolResult
		}
		cloned = append(cloned, copyMsg)
	}
	return cloned
}

func userTexts(messages []llm.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != llm.RoleUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeText {
				out = append(out, block.Text)
			}
		}
	}
	return out
}

func lastUserText(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != llm.RoleUser {
			continue
		}
		for j := len(msg.Content) - 1; j >= 0; j-- {
			block := msg.Content[j]
			if block.Type == llm.ContentTypeText {
				return block.Text
			}
		}
	}
	return ""
}

func eventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
