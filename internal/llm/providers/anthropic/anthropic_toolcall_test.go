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

// TestToolCallChunkedJSON verifies chunked tool JSON deltas are reassembled into valid arguments.
func TestToolCallChunkedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not implement flusher")
		}

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":12,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}

`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}}

`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\""}}

`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"main.go\"}"}}

`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}

`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":""},"usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}

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
					{Type: core.ContentTypeText, Text: "read file"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var gotEnd *core.ToolCall
	for ev := range stream {
		if ev.Type == core.EventToolCallEnd {
			gotEnd = ev.ToolCall
		}
	}
	if gotEnd == nil {
		t.Fatalf("expected EventToolCallEnd")
	}
	if gotEnd.ID != "toolu_1" || gotEnd.Name != "Read" {
		t.Fatalf("unexpected tool call identity: %+v", gotEnd)
	}
	if string(gotEnd.Arguments) != "{\"path\":\"main.go\"}" {
		t.Fatalf("unexpected tool arguments: %s", string(gotEnd.Arguments))
	}
}
