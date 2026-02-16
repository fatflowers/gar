package tui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"gar/internal/llm"
	"gar/internal/session"
)

func TestSessionRecorderPersistsRunSequence(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	rec, err := OpenSessionRecorder(context.Background(), store, "sess-1")
	if err != nil {
		t.Fatalf("OpenSessionRecorder() error = %v", err)
	}

	if err := rec.AppendMeta(context.Background(), map[string]any{"model": "claude-sonnet-4", "cwd": "/repo"}); err != nil {
		t.Fatalf("AppendMeta() error = %v", err)
	}
	if err := rec.AppendUser(context.Background(), "read main.go"); err != nil {
		t.Fatalf("AppendUser() error = %v", err)
	}

	events := []llm.Event{
		{
			Type: llm.EventToolCallStart,
			ToolCall: &llm.ToolCall{
				ID:        "tc-1",
				Name:      "read",
				Arguments: json.RawMessage(`{"path":"main.go"}`),
			},
		},
		{
			Type: llm.EventToolResult,
			ToolResult: &llm.ToolResult{
				ToolCallID: "tc-1",
				ToolName:   "read",
				Content:    "package main",
				IsError:    false,
			},
		},
		{
			Type:      llm.EventTextDelta,
			TextDelta: "Found main package.",
		},
		{
			Type:  llm.EventUsage,
			Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		},
		{
			Type: llm.EventDone,
			Done: &llm.DonePayload{Reason: llm.StopReasonStop},
		},
	}
	for _, ev := range events {
		if err := rec.RecordEvent(context.Background(), ev); err != nil {
			t.Fatalf("RecordEvent(%s) error = %v", ev.Type, err)
		}
	}

	entries, err := store.Load(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("entry count = %d, want 5", len(entries))
	}

	if entries[0].Type != "meta" {
		t.Fatalf("entry0 type = %q, want meta", entries[0].Type)
	}
	if entries[1].Type != "user" || entries[1].Content != "read main.go" {
		t.Fatalf("entry1 = %#v, want user content", entries[1])
	}
	if entries[2].Type != "tool_call" || entries[2].Name != "read" {
		t.Fatalf("entry2 = %#v, want tool_call read", entries[2])
	}
	if entries[3].Type != "tool_result" || entries[3].ToolCallID != "tc-1" {
		t.Fatalf("entry3 = %#v, want tool_result tc-1", entries[3])
	}
	if entries[4].Type != "assistant" || entries[4].Content != "Found main package." {
		t.Fatalf("entry4 = %#v, want assistant content", entries[4])
	}
	if len(entries[4].Usage) == 0 {
		t.Fatalf("assistant usage should be present")
	}
}

func TestSessionRecorderContinuesFromExistingSession(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.Append(context.Background(), "existing", session.Entry{
		ID:      "000001",
		Type:    "meta",
		Content: "",
		TS:      1700000001,
	}); err != nil {
		t.Fatalf("seed Append() error = %v", err)
	}

	rec, err := OpenSessionRecorder(context.Background(), store, "existing")
	if err != nil {
		t.Fatalf("OpenSessionRecorder() error = %v", err)
	}
	if err := rec.AppendUser(context.Background(), "next"); err != nil {
		t.Fatalf("AppendUser() error = %v", err)
	}

	entries, err := store.Load(context.Background(), "existing")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	if entries[1].ID != "000002" || entries[1].ParentID != "000001" {
		t.Fatalf("continued entry = %#v, want id=000002 parent=000001", entries[1])
	}
}
