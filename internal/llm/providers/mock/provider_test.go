package mockprovider

import (
	"context"
	"testing"

	"gar/internal/llm/core"
)

// TestMockProviderStreamsScriptedEvents verifies deterministic event ordering.
func TestMockProviderStreamsScriptedEvents(t *testing.T) {
	t.Parallel()

	mp := &Provider{
		Events: []core.Event{
			{Type: core.EventStart},
			{Type: core.EventTextDelta, TextDelta: "hello"},
			{
				Type: core.EventDone,
				Done: &core.DonePayload{
					Reason: core.StopReasonStop,
				},
			},
		},
	}

	stream, err := mp.Stream(context.Background(), &core.Request{Model: "mock"})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var got []core.EventType
	for ev := range stream {
		got = append(got, ev.Type)
	}

	want := []core.EventType{core.EventStart, core.EventTextDelta, core.EventDone}
	if len(got) != len(want) {
		t.Fatalf("event count mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d mismatch: got %s want %s", i, got[i], want[i])
		}
	}
}
