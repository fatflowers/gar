package core

import (
	"context"
	"errors"
	"testing"
)

func TestSendEventDelivered(t *testing.T) {
	t.Parallel()

	events := make(chan Event, 1)
	want := Event{Type: EventStart}
	if err := SendEvent(context.Background(), events, want); err != nil {
		t.Fatalf("SendEvent() error = %v", err)
	}
	got := <-events
	if got.Type != want.Type {
		t.Fatalf("event type = %q, want %q", got.Type, want.Type)
	}
}

func TestSendEventCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := SendEvent(ctx, make(chan Event), Event{Type: EventStart})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendEvent() error = %v, want context canceled", err)
	}
}

func TestSendTerminalEventAlwaysSends(t *testing.T) {
	t.Parallel()

	events := make(chan Event, 1)
	want := Event{Type: EventDone}
	SendTerminalEvent(events, want)

	got := <-events
	if got.Type != want.Type {
		t.Fatalf("event type = %q, want %q", got.Type, want.Type)
	}
}
