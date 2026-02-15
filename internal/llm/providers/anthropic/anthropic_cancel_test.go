package anthropicprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gar/internal/llm/core"
)

// TestStreamCancelReturnsAbortedError verifies cancellation maps to an aborted terminal reason.
func TestStreamCancelReturnsAbortedError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not implement flusher")
		}
		_, _ = fmt.Fprint(w, "\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := p.Stream(ctx, &core.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentTypeText, Text: "hello"}}},
		},
		MaxTokens: 128,
		Retry: core.RetryPolicy{
			MaxRetries: 1,
			BaseDelay:  5 * time.Millisecond,
			MaxDelay:   10 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var seenStart bool
	var seenAborted bool
	for ev := range stream {
		if ev.Type == core.EventStart {
			seenStart = true
			cancel()
		}
		if ev.Type == core.EventError && ev.Done != nil && ev.Done.Reason == core.StopReasonAborted {
			seenAborted = true
		}
	}

	if !seenStart {
		t.Fatalf("expected EventStart before cancellation")
	}
	if !seenAborted {
		t.Fatalf("expected aborted EventError after cancellation")
	}
}
