package llm

import (
	anthropicprovider "gar/internal/llm/providers/anthropic"
	mockprovider "gar/internal/llm/providers/mock"

	"gar/internal/llm/core"
)

type (
	// Provider is the public streaming provider contract.
	Provider = core.Provider

	// EventType enumerates stream event variants.
	EventType = core.EventType

	// ToolChoice* aliases expose tool-selection primitives.
	ToolChoiceType = core.ToolChoiceType
	ToolChoice     = core.ToolChoice
	ToolSpec       = core.ToolSpec
	RetryPolicy    = core.RetryPolicy

	// Request and Event payload aliases define the public stream protocol.
	Request           = core.Request
	DonePayload       = core.DonePayload
	ContentBlockStart = core.ContentBlockStart
	Event             = core.Event

	// Conversation-model aliases.
	Role        = core.Role
	StopReason  = core.StopReason
	ContentType = core.ContentType

	// Message and usage aliases.
	ContentBlock = core.ContentBlock
	ToolCall     = core.ToolCall
	ToolResult   = core.ToolResult
	Message      = core.Message
	Usage        = core.Usage

	// ModelPricing configures per-model token prices.
	ModelPricing = core.ModelPricing

	// Anthropic* aliases expose provider-specific configuration and implementation.
	AnthropicConfig   = anthropicprovider.Config
	AnthropicProvider = anthropicprovider.Provider

	// MockProvider emits scripted events for tests.
	MockProvider = mockprovider.Provider
)

const (
	EventStart             = core.EventStart
	EventContentBlockStart = core.EventContentBlockStart
	EventTextDelta         = core.EventTextDelta
	EventToolCallStart     = core.EventToolCallStart
	EventToolCallDelta     = core.EventToolCallDelta
	EventToolCallEnd       = core.EventToolCallEnd
	EventToolResult        = core.EventToolResult
	EventUsage             = core.EventUsage
	EventDone              = core.EventDone
	EventError             = core.EventError

	ToolChoiceAuto = core.ToolChoiceAuto
	ToolChoiceAny  = core.ToolChoiceAny
	ToolChoiceNone = core.ToolChoiceNone
	ToolChoiceTool = core.ToolChoiceTool

	RoleUser      = core.RoleUser
	RoleAssistant = core.RoleAssistant
	RoleTool      = core.RoleTool

	StopReasonStop    = core.StopReasonStop
	StopReasonLength  = core.StopReasonLength
	StopReasonToolUse = core.StopReasonToolUse
	StopReasonError   = core.StopReasonError
	StopReasonAborted = core.StopReasonAborted

	ContentTypeText = core.ContentTypeText
)

var (
	// ErrInvalidRequest indicates malformed canonical request payloads.
	ErrInvalidRequest = core.ErrInvalidRequest
	// ErrMissingAPIKey indicates missing Anthropic API credentials.
	ErrMissingAPIKey = core.ErrMissingAPIKey
)

// NewToolSpecFromStruct reflects a Go struct into a normalized tool schema.
func NewToolSpecFromStruct(name, description string, schemaStruct any) (ToolSpec, error) {
	return core.NewToolSpecFromStruct(name, description, schemaStruct)
}

// CalculateCost computes token usage cost in USD for a model pricing table.
func CalculateCost(u Usage, p ModelPricing) float64 {
	return core.CalculateCost(u, p)
}

// NewAnthropicProvider constructs an Anthropic provider with normalized defaults.
func NewAnthropicProvider(cfg AnthropicConfig) *AnthropicProvider {
	return anthropicprovider.New(cfg)
}
