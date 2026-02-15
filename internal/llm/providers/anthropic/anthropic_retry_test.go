package anthropicprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gar/internal/llm/core"
)

// TestRetryOn429BeforeFirstDelta verifies pre-output 429 responses are retried.
func TestRetryOn429BeforeFirstDelta(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"error":"rate limited"}`)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not implement flusher")
		}

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":""},"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}

`,
			`event: message_stop
data: {"type":"message_stop"}

`,
		}
		for _, chunk := range events {
			_, _ = fmt.Fprint(w, chunk)
			flusher.Flush()
		}
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
			MaxRetries: 2,
			BaseDelay:  10 * time.Millisecond,
			MaxDelay:   20 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var seenDone bool
	var startCount int
	var errorCount int
	for ev := range stream {
		if ev.Type == core.EventStart {
			startCount++
		}
		if ev.Type == core.EventDone {
			seenDone = true
		}
		if ev.Type == core.EventError {
			errorCount++
		}
	}
	if !seenDone {
		t.Fatalf("expected EventDone after retry")
	}
	if errorCount != 0 {
		t.Fatalf("expected no EventError, got %d", errorCount)
	}
	if startCount != 1 {
		t.Fatalf("expected exactly one EventStart, got %d", startCount)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

// TestNoRetryAfterFirstDelta verifies retries stop once visible output has been emitted.
func TestNoRetryAfterFirstDelta(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not implement flusher")
		}

		// Intentionally stop before message_stop to simulate transport failure.
		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":2,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}

`,
		}
		for _, chunk := range events {
			_, _ = fmt.Fprint(w, chunk)
			flusher.Flush()
		}
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
			MaxRetries: 3,
			BaseDelay:  10 * time.Millisecond,
			MaxDelay:   20 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var seenError bool
	for ev := range stream {
		if ev.Type == core.EventError {
			seenError = true
		}
	}
	if !seenError {
		t.Fatalf("expected EventError for mid-stream termination")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected only 1 attempt after first delta, got %d", got)
	}
}
