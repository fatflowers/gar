package anthropicprovider

import (
	"context"
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"gar/internal/llm/core"
)

// TestContentBlockStartSupportsAllSDKVariants verifies content_block_start mapping for all known block variants.
func TestContentBlockStartSupportsAllSDKVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		rawEvent          string
		wantBlockType     string
		wantToolCallStart bool
	}{
		{
			name:          "text",
			rawEvent:      `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			wantBlockType: "text",
		},
		{
			name:          "thinking",
			rawEvent:      `{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":"plan","signature":"sig"}}`,
			wantBlockType: "thinking",
		},
		{
			name:          "redacted_thinking",
			rawEvent:      `{"type":"content_block_start","index":2,"content_block":{"type":"redacted_thinking","data":"encrypted"}}`,
			wantBlockType: "redacted_thinking",
		},
		{
			name:              "tool_use",
			rawEvent:          `{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"main.go"}}}`,
			wantBlockType:     "tool_use",
			wantToolCallStart: true,
		},
		{
			name:          "server_tool_use",
			rawEvent:      `{"type":"content_block_start","index":4,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"go"}}}`,
			wantBlockType: "server_tool_use",
		},
		{
			name:          "web_search_tool_result",
			rawEvent:      `{"type":"content_block_start","index":5,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":{"type":"web_search_tool_result_error","error_code":"unavailable"}}}`,
			wantBlockType: "web_search_tool_result",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var sdkEvent anthropic.MessageStreamEventUnion
			if err := json.Unmarshal([]byte(tc.rawEvent), &sdkEvent); err != nil {
				t.Fatalf("unmarshal sdk event: %v", err)
			}

			p := &Provider{}
			state := &streamState{
				reason:           core.StopReasonStop,
				toolAccumulators: map[int]*toolCallAccumulator{},
			}
			events := make(chan core.Event, 4)

			if err := p.handleSDKStreamEvent(context.Background(), sdkEvent, "claude-sonnet-4", events, state); err != nil {
				t.Fatalf("handleSDKStreamEvent() error = %v", err)
			}

			gotEvents := drainEvents(events)
			if len(gotEvents) == 0 {
				t.Fatalf("expected at least one event")
			}
			if gotEvents[0].Type != core.EventContentBlockStart {
				t.Fatalf("first event type = %q, want %q", gotEvents[0].Type, core.EventContentBlockStart)
			}
			if gotEvents[0].ContentBlockStart == nil {
				t.Fatalf("expected content block payload")
			}
			if gotEvents[0].ContentBlockStart.Type != tc.wantBlockType {
				t.Fatalf("content block type = %q, want %q", gotEvents[0].ContentBlockStart.Type, tc.wantBlockType)
			}

			var seenToolCallStart bool
			for _, ev := range gotEvents {
				if ev.Type == core.EventToolCallStart {
					seenToolCallStart = true
				}
			}
			if seenToolCallStart != tc.wantToolCallStart {
				t.Fatalf("tool call start presence = %v, want %v", seenToolCallStart, tc.wantToolCallStart)
			}
		})
	}
}

// drainEvents reads all currently buffered events from the channel.
func drainEvents(ch <-chan core.Event) []core.Event {
	out := make([]core.Event, 0, len(ch))
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}
