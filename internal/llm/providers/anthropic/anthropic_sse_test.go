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

// TestStreamEmitsTextDeltaAndDone verifies basic text streaming emits delta and done events.
func TestStreamEmitsTextDeltaAndDone(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not implement flusher")
		}

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}

`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":""},"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}

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
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 128,
		Messages: []core.Message{
			{
				Role: core.RoleUser,
				Content: []core.ContentBlock{
					{Type: core.ContentTypeText, Text: "hello"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var seenDelta, seenDone bool
	for ev := range stream {
		if ev.Type == core.EventTextDelta && ev.TextDelta == "hi" {
			seenDelta = true
		}
		if ev.Type == core.EventDone {
			seenDone = true
		}
	}
	if !seenDelta || !seenDone {
		t.Fatalf("expected delta+done events, got delta=%v done=%v", seenDelta, seenDone)
	}
}
