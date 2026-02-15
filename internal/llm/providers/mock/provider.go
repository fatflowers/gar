package mockprovider

import (
	"context"
	"time"

	"gar/internal/llm/core"
)

// Provider emits a predefined event script for deterministic tests.
type Provider struct {
	Events []core.Event
	Delay  time.Duration
}

// Stream emits scripted events in order until exhaustion or cancellation.
func (m *Provider) Stream(ctx context.Context, req *core.Request) (<-chan core.Event, error) {
	_ = req

	out := make(chan core.Event, 1)
	go func() {
		defer close(out)
		for _, ev := range m.Events {
			if m.Delay > 0 {
				timer := time.NewTimer(m.Delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					core.SendTerminalEvent(out, core.Event{
						Type: core.EventError,
						Done: &core.DonePayload{Reason: core.StopReasonAborted},
						Err:  ctx.Err(),
					})
					return
				case <-timer.C:
				}
			}

			select {
			case <-ctx.Done():
				core.SendTerminalEvent(out, core.Event{
					Type: core.EventError,
					Done: &core.DonePayload{Reason: core.StopReasonAborted},
					Err:  ctx.Err(),
				})
				return
			case out <- ev:
			}
		}
	}()

	return out, nil
}
