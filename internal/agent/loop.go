package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gar/internal/llm"
)

type runLoopHooks struct {
	dequeueSteeringMessages func() []llm.Message
	dequeueFollowUpMessages func() []llm.Message
	executeToolCall         func(ctx context.Context, call llm.ToolCall) (llm.Message, error)
}

func runLoop(
	ctx context.Context,
	provider llm.Provider,
	req *llm.Request,
	maxTurns int,
	out chan<- llm.Event,
	hooks runLoopHooks,
) (terminalForwarded bool, err error) {
	if req == nil {
		return false, ErrRequestRequired
	}

	if maxTurns <= 0 {
		maxTurns = 1
	}

	pendingMessages := dequeueMessages(hooks.dequeueSteeringMessages)
	if len(pendingMessages) == 0 {
		pendingMessages = dequeueMessages(hooks.dequeueFollowUpMessages)
	}

	for turn := 0; turn < maxTurns; turn++ {
		if len(pendingMessages) > 0 {
			req.Messages = append(req.Messages, cloneMessages(pendingMessages)...)
			pendingMessages = nil
		}

		stream, err := provider.Stream(ctx, req)
		if err != nil {
			return false, err
		}

		terminal, hasTerminal, assistantMessage, err := forwardProviderEvents(ctx, stream, out)
		if err != nil {
			return false, err
		}
		if !hasTerminal {
			return false, errors.New("provider stream ended without terminal event")
		}
		if assistantMessage != nil {
			req.Messages = append(req.Messages, *assistantMessage)
		}

		switch terminal.Type {
		case llm.EventError:
			// Error terminal was already emitted by provider stream.
			return true, nil
		}

		if terminal.Done != nil && terminal.Done.Reason == llm.StopReasonToolUse {
			if hooks.executeToolCall == nil || assistantMessage == nil || len(assistantMessage.ToolCalls) == 0 {
				return true, nil
			}

			for i, toolCall := range assistantMessage.ToolCalls {
				call := cloneToolCall(toolCall)
				if err := sendStreamEvent(ctx, out, llm.Event{
					Type:     llm.EventToolCallStart,
					ToolCall: &call,
				}); err != nil {
					return false, err
				}

				toolResultMessage, err := hooks.executeToolCall(ctx, call)
				if err != nil {
					return false, err
				}
				req.Messages = append(req.Messages, toolResultMessage)
				if toolResultMessage.ToolResult != nil {
					toolResult := *toolResultMessage.ToolResult
					if err := sendStreamEvent(ctx, out, llm.Event{
						Type:       llm.EventToolResult,
						ToolResult: &toolResult,
					}); err != nil {
						return false, err
					}
				}

				if err := sendStreamEvent(ctx, out, llm.Event{
					Type:     llm.EventToolCallEnd,
					ToolCall: &call,
				}); err != nil {
					return false, err
				}

				if steering := dequeueMessages(hooks.dequeueSteeringMessages); len(steering) > 0 {
					pendingMessages = steering
					remainingCalls := assistantMessage.ToolCalls[i+1:]
					for _, remaining := range remainingCalls {
						skippedCall := cloneToolCall(remaining)
						if err := sendStreamEvent(ctx, out, llm.Event{
							Type:     llm.EventToolCallStart,
							ToolCall: &skippedCall,
						}); err != nil {
							return false, err
						}

						skippedResultMessage := skipToolCall(skippedCall)
						req.Messages = append(req.Messages, skippedResultMessage)

						skippedResult := *skippedResultMessage.ToolResult
						if err := sendStreamEvent(ctx, out, llm.Event{
							Type:       llm.EventToolResult,
							ToolResult: &skippedResult,
						}); err != nil {
							return false, err
						}

						if err := sendStreamEvent(ctx, out, llm.Event{
							Type:     llm.EventToolCallEnd,
							ToolCall: &skippedCall,
						}); err != nil {
							return false, err
						}
					}
					break
				}
			}
			continue
		}

		if steering := dequeueMessages(hooks.dequeueSteeringMessages); len(steering) > 0 {
			pendingMessages = steering
			continue
		}
		if followUp := dequeueMessages(hooks.dequeueFollowUpMessages); len(followUp) > 0 {
			pendingMessages = followUp
			continue
		}

		return true, nil
	}

	return false, ErrMaxTurnsExceeded
}

func forwardProviderEvents(
	ctx context.Context,
	stream <-chan llm.Event,
	out chan<- llm.Event,
) (terminal llm.Event, hasTerminal bool, assistantMessage *llm.Message, err error) {
	accumulator := newAssistantAccumulator()

	for {
		select {
		case <-ctx.Done():
			return llm.Event{}, false, nil, ctx.Err()
		case ev, ok := <-stream:
			if !ok {
				return llm.Event{}, false, nil, nil
			}

			if err := sendStreamEvent(ctx, out, ev); err != nil {
				return llm.Event{}, false, nil, err
			}

			accumulator.consume(ev)
			if ev.Type == llm.EventDone || ev.Type == llm.EventError {
				return ev, true, accumulator.buildMessage(), nil
			}
		}
	}
}

// forwardEvents decouples producer and consumer backpressure so abandoned
// consumers do not block loop teardown. It flushes remaining queued events
// on close only while the output channel can accept without blocking.
func forwardEvents(in <-chan llm.Event, out chan<- llm.Event) {
	queue := make([]llm.Event, 0, 8)

	for {
		var next llm.Event
		var outCh chan<- llm.Event
		if len(queue) > 0 {
			next = queue[0]
			outCh = out
		}

		select {
		case ev, ok := <-in:
			if !ok {
				for len(queue) > 0 {
					timer := time.NewTimer(forwardFlushWait)
					select {
					case out <- queue[0]:
						queue = queue[1:]
						if !timer.Stop() {
							<-timer.C
						}
					case <-timer.C:
						return
					}
				}
				return
			}
			queue = append(queue, ev)
		case outCh <- next:
			queue = queue[1:]
		}
	}
}

func dequeueMessages(fn func() []llm.Message) []llm.Message {
	if fn == nil {
		return nil
	}
	return fn()
}

func sendStreamEvent(ctx context.Context, out chan<- llm.Event, ev llm.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- ev:
		return nil
	}
}

type assistantAccumulator struct {
	text          strings.Builder
	toolCallOrder []string
	toolCallsByID map[string]llm.ToolCall
}

func newAssistantAccumulator() *assistantAccumulator {
	return &assistantAccumulator{
		toolCallsByID: make(map[string]llm.ToolCall),
	}
}

func (a *assistantAccumulator) consume(ev llm.Event) {
	switch ev.Type {
	case llm.EventContentBlockStart:
		if ev.ContentBlockStart != nil && ev.ContentBlockStart.Type == "text" && ev.ContentBlockStart.Text != "" {
			a.text.WriteString(ev.ContentBlockStart.Text)
		}
	case llm.EventTextDelta:
		a.text.WriteString(ev.TextDelta)
	case llm.EventToolCallStart, llm.EventToolCallEnd:
		if ev.ToolCall != nil {
			a.upsertToolCall(*ev.ToolCall)
		}
	}
}

func (a *assistantAccumulator) upsertToolCall(call llm.ToolCall) {
	if _, exists := a.toolCallsByID[call.ID]; !exists {
		a.toolCallOrder = append(a.toolCallOrder, call.ID)
	}
	a.toolCallsByID[call.ID] = cloneToolCall(call)
}

func (a *assistantAccumulator) buildMessage() *llm.Message {
	var toolCalls []llm.ToolCall
	if len(a.toolCallOrder) > 0 {
		toolCalls = make([]llm.ToolCall, 0, len(a.toolCallOrder))
		for _, id := range a.toolCallOrder {
			call, ok := a.toolCallsByID[id]
			if !ok {
				continue
			}
			toolCalls = append(toolCalls, call)
		}
	}

	if a.text.Len() == 0 && len(toolCalls) == 0 {
		return nil
	}

	message := llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: toolCalls,
	}
	if a.text.Len() > 0 {
		message.Content = []llm.ContentBlock{
			{
				Type: llm.ContentTypeText,
				Text: a.text.String(),
			},
		}
	}

	return &message
}

func cloneToolCall(call llm.ToolCall) llm.ToolCall {
	cloned := call
	cloned.Arguments = append(json.RawMessage(nil), call.Arguments...)
	return cloned
}

func skipToolCall(call llm.ToolCall) llm.Message {
	return llm.Message{
		Role: llm.RoleTool,
		ToolResult: &llm.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    skippedToolCallMessage,
			IsError:    true,
		},
	}
}

const forwardFlushWait = 50 * time.Millisecond
const skippedToolCallMessage = "Skipped due to queued user message."
