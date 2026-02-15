package anthropicprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"gar/internal/llm/core"
)

// Config configures the Anthropic provider.
type Config struct {
	APIKey       string
	BaseURL      string
	Version      string
	HTTPClient   *http.Client
	Retry        core.RetryPolicy
	ModelPricing map[string]core.ModelPricing
}

// Provider is a thin wrapper around the official anthropic-sdk-go client.
type Provider struct {
	apiKey  string
	retry   core.RetryPolicy
	pricing map[string]core.ModelPricing

	client anthropic.Client
}

// New constructs a provider with sane defaults.
func New(cfg Config) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	version := strings.TrimSpace(cfg.Version)

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}

	pricing := cfg.ModelPricing
	if pricing == nil {
		pricing = map[string]core.ModelPricing{}
	}

	apiKey := strings.TrimSpace(cfg.APIKey)
	clientOptions := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0), // explicit retry behavior in this package
	}
	if baseURL != "" {
		clientOptions = append(clientOptions, option.WithBaseURL(baseURL))
	}
	if version != "" {
		clientOptions = append(clientOptions, option.WithHeader("anthropic-version", version))
	}

	return &Provider{
		apiKey:  apiKey,
		retry:   core.NormalizeRetryPolicy(cfg.Retry),
		pricing: pricing,
		client:  anthropic.NewClient(clientOptions...),
	}
}

// Stream executes a single Anthropic Messages API streaming request.
func (p *Provider) Stream(ctx context.Context, req *core.Request) (<-chan core.Event, error) {
	if p == nil {
		return nil, fmt.Errorf("anthropic provider is nil")
	}
	if strings.TrimSpace(p.apiKey) == "" {
		return nil, core.ErrMissingAPIKey
	}

	params, err := toAnthropicSDKParams(req)
	if err != nil {
		return nil, err
	}

	events := make(chan core.Event, 1)
	retry := core.MergeRetryPolicy(p.retry, req.Retry)

	go func() {
		defer close(events)
		state := &streamState{reason: core.StopReasonStop}
		if err := p.streamWithRetry(ctx, params, req.Model, retry, events, state); err != nil {
			reason := core.StopReasonError
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				reason = core.StopReasonAborted
			}
			core.SendTerminalEvent(events, core.Event{
				Type: core.EventError,
				Done: &core.DonePayload{
					Reason: reason,
					Usage:  state.usage,
				},
				Err: fmt.Errorf("anthropic stream: %w", err),
			})
		}
	}()

	return events, nil
}

// streamState tracks incremental response state across one logical stream request.
type streamState struct {
	usage            core.Usage
	reason           core.StopReason
	emittedVisible   bool
	startEmitted     bool
	emittedDone      bool
	toolAccumulators map[int]*toolCallAccumulator
}

// toolCallAccumulator incrementally reconstructs chunked JSON tool arguments.
type toolCallAccumulator struct {
	id   string
	name string
	buf  strings.Builder
}

// streamWithRetry retries failed streams only when no visible output has been emitted yet.
func (p *Provider) streamWithRetry(
	ctx context.Context,
	params anthropic.MessageNewParams,
	model string,
	retry core.RetryPolicy,
	events chan<- core.Event,
	state *streamState,
) error {
	attempt := 0
	for {
		attemptErr := p.streamOnce(ctx, params, model, events, state)
		if attemptErr == nil {
			return nil
		}
		if errors.Is(attemptErr, context.Canceled) || errors.Is(attemptErr, context.DeadlineExceeded) {
			return attemptErr
		}
		if !core.IsRetryableError(attemptErr) || state.emittedVisible || attempt >= retry.MaxRetries {
			return attemptErr
		}

		delay := core.ComputeBackoffDelay(retry, attempt)
		if err := core.SleepContext(ctx, delay); err != nil {
			return err
		}
		attempt++
	}
}

// streamOnce consumes one SDK stream and emits canonical events.
func (p *Provider) streamOnce(
	ctx context.Context,
	params anthropic.MessageNewParams,
	model string,
	events chan<- core.Event,
	state *streamState,
) error {
	stream := p.client.Messages.NewStreaming(ctx, params)
	defer func() {
		_ = stream.Close()
	}()

	if !state.startEmitted {
		if err := core.SendEvent(ctx, events, core.Event{Type: core.EventStart}); err != nil {
			return err
		}
		state.startEmitted = true
	}

	if state.toolAccumulators == nil {
		state.toolAccumulators = map[int]*toolCallAccumulator{}
	}

	for stream.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		event := stream.Current()
		if err := p.handleSDKStreamEvent(ctx, event, model, events, state); err != nil {
			return err
		}
		if state.emittedDone {
			return nil
		}
	}

	if err := stream.Err(); err != nil {
		wrapped := fmt.Errorf("anthropic sdk stream: %w", err)
		if isRetryableProviderError(err) {
			return core.MarkRetryable(wrapped)
		}
		return wrapped
	}

	if state.emittedDone {
		return nil
	}

	return core.MarkRetryable(errors.New("anthropic stream ended without message_stop"))
}

// handleSDKStreamEvent maps raw Anthropic stream events into canonical event payloads.
func (p *Provider) handleSDKStreamEvent(
	ctx context.Context,
	event anthropic.MessageStreamEventUnion,
	model string,
	events chan<- core.Event,
	state *streamState,
) error {
	switch variant := event.AsAny().(type) {
	case anthropic.MessageStartEvent:
		applyStartUsage(&state.usage, variant.Message.Usage)
		state.usage.TotalTokens = state.usage.TokenCount()
		state.usage.CostUSD = p.calculateCost(model, state.usage)
		return core.SendEvent(ctx, events, core.Event{Type: core.EventUsage, Usage: state.usage.Clone()})

	case anthropic.ContentBlockStartEvent:
		switch block := variant.ContentBlock.AsAny().(type) {
		case anthropic.TextBlock:
			start := &core.ContentBlockStart{
				Index: variant.Index,
				Type:  string(block.Type),
				Text:  block.Text,
				Raw:   core.RawJSONFromString(variant.ContentBlock.RawJSON()),
			}
			return core.SendEvent(ctx, events, core.Event{
				Type:              core.EventContentBlockStart,
				ContentBlockStart: start,
			})

		case anthropic.ThinkingBlock:
			start := &core.ContentBlockStart{
				Index:     variant.Index,
				Type:      string(block.Type),
				Thinking:  block.Thinking,
				Signature: block.Signature,
				Raw:       core.RawJSONFromString(variant.ContentBlock.RawJSON()),
			}
			return core.SendEvent(ctx, events, core.Event{
				Type:              core.EventContentBlockStart,
				ContentBlockStart: start,
			})

		case anthropic.RedactedThinkingBlock:
			start := &core.ContentBlockStart{
				Index: variant.Index,
				Type:  string(block.Type),
				Data:  block.Data,
				Raw:   core.RawJSONFromString(variant.ContentBlock.RawJSON()),
			}
			return core.SendEvent(ctx, events, core.Event{
				Type:              core.EventContentBlockStart,
				ContentBlockStart: start,
			})

		case anthropic.ToolUseBlock:
			rawInput, err := core.MarshalToolInput(block.Input)
			if err != nil {
				return fmt.Errorf("marshal tool_use input: %w", err)
			}

			acc := &toolCallAccumulator{id: block.ID, name: block.Name}
			if len(rawInput) > 0 && string(rawInput) != "{}" {
				_, _ = acc.buf.Write(rawInput)
			}
			state.toolAccumulators[int(variant.Index)] = acc
			state.emittedVisible = true

			start := &core.ContentBlockStart{
				Index: variant.Index,
				Type:  string(block.Type),
				ID:    block.ID,
				Name:  block.Name,
				Input: append(json.RawMessage(nil), rawInput...),
				Raw:   core.RawJSONFromString(variant.ContentBlock.RawJSON()),
			}
			if err := core.SendEvent(ctx, events, core.Event{
				Type:              core.EventContentBlockStart,
				ContentBlockStart: start,
			}); err != nil {
				return err
			}
			return core.SendEvent(ctx, events, core.Event{
				Type: core.EventToolCallStart,
				ToolCall: &core.ToolCall{
					ID:        block.ID,
					Name:      block.Name,
					Arguments: append(json.RawMessage(nil), rawInput...),
				},
			})

		case anthropic.ServerToolUseBlock:
			rawInput, err := core.MarshalToolInput(block.Input)
			if err != nil {
				return fmt.Errorf("marshal server_tool_use input: %w", err)
			}
			start := &core.ContentBlockStart{
				Index: variant.Index,
				Type:  string(block.Type),
				ID:    block.ID,
				Name:  string(block.Name),
				Input: append(json.RawMessage(nil), rawInput...),
				Raw:   core.RawJSONFromString(variant.ContentBlock.RawJSON()),
			}
			return core.SendEvent(ctx, events, core.Event{
				Type:              core.EventContentBlockStart,
				ContentBlockStart: start,
			})

		case anthropic.WebSearchToolResultBlock:
			start := &core.ContentBlockStart{
				Index:     variant.Index,
				Type:      string(block.Type),
				ToolUseID: block.ToolUseID,
				Raw:       core.RawJSONFromString(variant.ContentBlock.RawJSON()),
			}
			return core.SendEvent(ctx, events, core.Event{
				Type:              core.EventContentBlockStart,
				ContentBlockStart: start,
			})
		default:
			return fmt.Errorf("unsupported content_block_start block: %T", block)
		}

	case anthropic.ContentBlockDeltaEvent:
		switch delta := variant.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			state.emittedVisible = true
			return core.SendEvent(ctx, events, core.Event{Type: core.EventTextDelta, TextDelta: delta.Text})
		case anthropic.InputJSONDelta:
			acc, ok := state.toolAccumulators[int(variant.Index)]
			if !ok {
				return fmt.Errorf("tool_call accumulator not found for index %d", variant.Index)
			}
			_, _ = acc.buf.WriteString(delta.PartialJSON)
			state.emittedVisible = true
			return core.SendEvent(ctx, events, core.Event{Type: core.EventToolCallDelta, ToolCallDelta: delta.PartialJSON})
		default:
			return nil
		}

	case anthropic.ContentBlockStopEvent:
		acc, ok := state.toolAccumulators[int(variant.Index)]
		if !ok {
			return nil
		}
		delete(state.toolAccumulators, int(variant.Index))

		rawArgs := bytes.TrimSpace([]byte(acc.buf.String()))
		if len(rawArgs) == 0 {
			rawArgs = []byte("{}")
		}
		if !json.Valid(rawArgs) {
			return fmt.Errorf("tool_call arguments are not valid JSON")
		}

		state.emittedVisible = true
		return core.SendEvent(ctx, events, core.Event{
			Type: core.EventToolCallEnd,
			ToolCall: &core.ToolCall{
				ID:        acc.id,
				Name:      acc.name,
				Arguments: append(json.RawMessage(nil), rawArgs...),
			},
		})

	case anthropic.MessageDeltaEvent:
		if variant.Delta.StopReason != "" {
			reason, err := mapStopReason(string(variant.Delta.StopReason))
			if err != nil {
				return err
			}
			state.reason = reason
		}
		applyDeltaUsage(&state.usage, variant.Usage)
		state.usage.TotalTokens = state.usage.TokenCount()
		state.usage.CostUSD = p.calculateCost(model, state.usage)
		return core.SendEvent(ctx, events, core.Event{Type: core.EventUsage, Usage: state.usage.Clone()})

	case anthropic.MessageStopEvent:
		state.emittedDone = true
		return core.SendEvent(ctx, events, core.Event{
			Type: core.EventDone,
			Done: &core.DonePayload{
				Reason: state.reason,
				Usage:  state.usage,
			},
		})
	}

	return nil
}

// calculateCost returns computed cost when pricing is configured for the requested model.
func (p *Provider) calculateCost(model string, usage core.Usage) float64 {
	pricing, ok := p.pricing[model]
	if !ok {
		return 0
	}
	return core.CalculateCost(usage, pricing)
}
