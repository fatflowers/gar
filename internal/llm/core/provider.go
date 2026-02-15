package core

import (
	"context"
	"encoding/json"
	"time"
)

// Provider streams model events for a single request.
type Provider interface {
	Stream(ctx context.Context, req *Request) (<-chan Event, error)
}

// EventType identifies stream event variants.
type EventType string

const (
	EventStart             EventType = "start"
	EventContentBlockStart EventType = "content_block_start"
	EventTextDelta         EventType = "text_delta"
	EventToolCallStart     EventType = "tool_call_start"
	EventToolCallDelta     EventType = "tool_call_delta"
	EventToolCallEnd       EventType = "tool_call_end"
	EventUsage             EventType = "usage"
	EventDone              EventType = "done"
	EventError             EventType = "error"
)

// ToolChoiceType defines how the provider may choose tools.
type ToolChoiceType string

const (
	ToolChoiceAuto ToolChoiceType = "auto"
	ToolChoiceAny  ToolChoiceType = "any"
	ToolChoiceNone ToolChoiceType = "none"
	ToolChoiceTool ToolChoiceType = "tool"
)

// ToolChoice controls provider tool dispatch mode.
type ToolChoice struct {
	Type ToolChoiceType `json:"type"`
	Name string         `json:"name,omitempty"`
}

// ToolSpec describes a tool exposed to the model.
// Schema can be generated from a Go struct via NewToolSpecFromStruct.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// RetryPolicy configures retry/backoff behavior for retryable failures.
type RetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// Request is the provider-agnostic streaming request.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	MaxTokens   int
	Temperature *float64
	ToolChoice  ToolChoice
	Metadata    map[string]string
	Retry       RetryPolicy
}

// DonePayload carries the final status when the stream ends normally.
type DonePayload struct {
	Reason StopReason
	Usage  Usage
}

// ContentBlockStart describes provider-native content block metadata.
type ContentBlockStart struct {
	Index     int64           `json:"index"`
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// Event is the provider-agnostic streaming event.
type Event struct {
	Type              EventType
	ContentBlockStart *ContentBlockStart
	TextDelta         string
	ToolCall          *ToolCall
	ToolCallDelta     string
	Usage             *Usage
	Done              *DonePayload
	Err               error
}
