package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gar/internal/llm"
	"gar/internal/session"
)

var (
	ErrRecorderStoreRequired = errors.New("session recorder store is required")
	ErrRecorderSessionID     = errors.New("session recorder session id is required")
)

// SessionRecorder persists user/assistant/tool events to session JSONL.
type SessionRecorder struct {
	store     *session.Store
	sessionID string

	mu            sync.Mutex
	nextEntryID   int
	parentEntryID string
	assistantText strings.Builder
	latestUsage   *llm.Usage
}

// OpenSessionRecorder attaches to an existing session or starts a new one.
func OpenSessionRecorder(ctx context.Context, store *session.Store, sessionID string) (*SessionRecorder, error) {
	if store == nil {
		return nil, ErrRecorderStoreRequired
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, ErrRecorderSessionID
	}

	rec := &SessionRecorder{
		store:       store,
		sessionID:   id,
		nextEntryID: 1,
	}

	entries, err := store.Load(ctx, id)
	if err != nil {
		if !errors.Is(err, session.ErrSessionNotFound) {
			return nil, err
		}
		return rec, nil
	}
	if len(entries) > 0 {
		rec.nextEntryID = len(entries) + 1
		rec.parentEntryID = entries[len(entries)-1].ID
	}
	return rec, nil
}

// AppendMeta writes a metadata entry (model/cwd/etc).
func (r *SessionRecorder) AppendMeta(ctx context.Context, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.appendLocked(ctx, session.Entry{
		Type: "meta",
		Data: raw,
	})
}

// AppendUser writes a user message entry.
func (r *SessionRecorder) AppendUser(ctx context.Context, content string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.appendLocked(ctx, session.Entry{
		Type:    "user",
		Content: content,
	})
}

// RecordEvent consumes one llm event and persists relevant session entries.
func (r *SessionRecorder) RecordEvent(ctx context.Context, ev llm.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch ev.Type {
	case llm.EventContentBlockStart:
		if ev.ContentBlockStart != nil && ev.ContentBlockStart.Type == "text" && ev.ContentBlockStart.Text != "" {
			r.assistantText.WriteString(ev.ContentBlockStart.Text)
		}
		return nil
	case llm.EventTextDelta:
		r.assistantText.WriteString(ev.TextDelta)
		return nil
	case llm.EventToolCallStart:
		if ev.ToolCall == nil {
			return nil
		}
		return r.appendLocked(ctx, session.Entry{
			Type:   "tool_call",
			Name:   ev.ToolCall.Name,
			Params: append(json.RawMessage(nil), ev.ToolCall.Arguments...),
		})
	case llm.EventToolResult:
		if ev.ToolResult == nil {
			return nil
		}
		state, err := json.Marshal(map[string]any{"is_error": ev.ToolResult.IsError})
		if err != nil {
			return fmt.Errorf("marshal tool_result state: %w", err)
		}
		return r.appendLocked(ctx, session.Entry{
			Type:       "tool_result",
			ToolCallID: ev.ToolResult.ToolCallID,
			Name:       ev.ToolResult.ToolName,
			Content:    ev.ToolResult.Content,
			Data:       state,
		})
	case llm.EventUsage:
		if ev.Usage != nil {
			usage := *ev.Usage
			r.latestUsage = &usage
		}
		return nil
	case llm.EventDone, llm.EventError:
		return r.flushAssistantLocked(ctx)
	default:
		return nil
	}
}

// Finalize flushes any pending assistant text when stream closes unexpectedly.
func (r *SessionRecorder) Finalize(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flushAssistantLocked(ctx)
}

func (r *SessionRecorder) flushAssistantLocked(ctx context.Context) error {
	content := strings.TrimSpace(r.assistantText.String())
	if content == "" {
		return nil
	}

	entry := session.Entry{
		Type:    "assistant",
		Content: content,
	}
	if r.latestUsage != nil {
		raw, err := json.Marshal(r.latestUsage)
		if err != nil {
			return fmt.Errorf("marshal usage: %w", err)
		}
		entry.Usage = raw
	}
	if err := r.appendLocked(ctx, entry); err != nil {
		return err
	}
	r.assistantText.Reset()
	r.latestUsage = nil
	return nil
}

func (r *SessionRecorder) appendLocked(ctx context.Context, entry session.Entry) error {
	entry.ID = fmt.Sprintf("%06d", r.nextEntryID)
	entry.ParentID = r.parentEntryID
	if entry.TS <= 0 {
		entry.TS = time.Now().Unix()
	}

	if err := r.store.Append(ctx, r.sessionID, entry); err != nil {
		return err
	}

	r.parentEntryID = entry.ID
	r.nextEntryID++
	return nil
}
