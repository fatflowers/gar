package core

import "context"

// SendEvent forwards an event unless the context has already been canceled.
func SendEvent(ctx context.Context, events chan<- Event, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case events <- event:
		return nil
	}
}

// SendTerminalEvent emits a terminal event without cancellation checks.
// The events channel must have buffer capacity of at least 1 so that
// the goroutine does not hang when the consumer has stopped reading.
func SendTerminalEvent(events chan<- Event, event Event) {
	select {
	case events <- event:
	default:
	}
}
