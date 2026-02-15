package anthropicprovider

import (
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"gar/internal/llm/core"
)

// defaultMaxTokens is used when callers do not provide an explicit token budget.
const defaultMaxTokens = 1024

// mapStopReason maps Anthropic stop reasons to canonical provider-agnostic values.
func mapStopReason(reason string) (core.StopReason, error) {
	switch reason {
	case "end_turn", "stop_sequence", "pause_turn":
		return core.StopReasonStop, nil
	case "max_tokens":
		return core.StopReasonLength, nil
	case "tool_use":
		return core.StopReasonToolUse, nil
	case "refusal", "sensitive":
		return core.StopReasonError, nil
	default:
		return "", fmt.Errorf("unhandled stop reason: %s", reason)
	}
}

// toAnthropicSDKParams validates and converts a canonical request into SDK params.
func toAnthropicSDKParams(req *core.Request) (anthropic.MessageNewParams, error) {
	if req == nil {
		return anthropic.MessageNewParams{}, fmt.Errorf("%w: request is nil", core.ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		return anthropic.MessageNewParams{}, fmt.Errorf("%w: model is required", core.ErrInvalidRequest)
	}

	messages, err := toSDKMessages(req.Messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	if strings.TrimSpace(req.System) != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Temperature)
	}
	if len(req.Tools) > 0 {
		tools, err := toSDKTools(req.Tools)
		if err != nil {
			return anthropic.MessageNewParams{}, err
		}
		params.Tools = tools
	}
	if toolChoice, ok := toSDKToolChoice(req.ToolChoice); ok {
		params.ToolChoice = toolChoice
	}
	if userID := strings.TrimSpace(req.Metadata["user_id"]); userID != "" {
		params.Metadata = anthropic.MetadataParam{UserID: anthropic.String(userID)}
	}

	return params, nil
}

// toSDKMessages converts canonical conversation messages into Anthropic SDK messages.
func toSDKMessages(messages []core.Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		switch msg.Role {
		case core.RoleUser:
			blocks := toSDKTextBlocks(msg.Content)
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
		case core.RoleAssistant:
			blocks := toSDKAssistantBlocks(msg)
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		case core.RoleTool:
			blocks, next, err := collectSDKToolResultBlocks(messages, i)
			if err != nil {
				return nil, err
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
			i = next
		default:
			return nil, fmt.Errorf("%w: unsupported role %q", core.ErrInvalidRequest, msg.Role)
		}
	}

	return out, nil
}

// toSDKTextBlocks keeps only non-empty text blocks supported by this integration.
func toSDKTextBlocks(content []core.ContentBlock) []anthropic.ContentBlockParamUnion {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(content))
	for _, item := range content {
		if item.Type != core.ContentTypeText {
			continue
		}
		text := item.Text
		if text == "" {
			continue
		}
		blocks = append(blocks, anthropic.NewTextBlock(text))
	}
	return blocks
}

// toSDKAssistantBlocks builds assistant blocks, including tool_use blocks when present.
func toSDKAssistantBlocks(msg core.Message) []anthropic.ContentBlockParamUnion {
	blocks := toSDKTextBlocks(msg.Content)
	for _, call := range msg.ToolCalls {
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
			continue
		}
		input := core.DecodeJSONObjectOrEmpty(call.Arguments)
		blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, input, call.Name))
	}
	return blocks
}

// collectSDKToolResultBlocks groups consecutive tool-result messages into one SDK user message.
func collectSDKToolResultBlocks(messages []core.Message, start int) ([]anthropic.ContentBlockParamUnion, int, error) {
	blocks := make([]anthropic.ContentBlockParamUnion, 0)
	last := start

	for j := start; j < len(messages); j++ {
		msg := messages[j]
		if msg.Role != core.RoleTool {
			break
		}
		last = j

		if msg.ToolResult == nil {
			continue
		}

		tr := msg.ToolResult
		if strings.TrimSpace(tr.ToolCallID) == "" {
			return nil, 0, fmt.Errorf("%w: tool result missing tool_call_id", core.ErrInvalidRequest)
		}

		blocks = append(blocks, anthropic.NewToolResultBlock(tr.ToolCallID, tr.Content, tr.IsError))
	}

	return blocks, last, nil
}

// toSDKTools converts canonical tool specs into Anthropic SDK tool definitions.
func toSDKTools(tools []core.ToolSpec) ([]anthropic.ToolUnionParam, error) {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		schema, err := core.DecodeToolJSONSchema(tool.Schema)
		if err != nil {
			return nil, fmt.Errorf("decode tool schema for %q: %w", tool.Name, err)
		}
		inputSchema := anthropic.ToolInputSchemaParam{
			Properties: schema.Properties,
			Required:   schema.Required,
		}
		toolParam := anthropic.ToolParam{
			Name:        tool.Name,
			InputSchema: inputSchema,
		}
		if strings.TrimSpace(tool.Description) != "" {
			toolParam.Description = anthropic.String(tool.Description)
		}

		out = append(out, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	return out, nil
}

// toSDKToolChoice maps canonical tool choice behavior to Anthropic SDK union params.
func toSDKToolChoice(choice core.ToolChoice) (anthropic.ToolChoiceUnionParam, bool) {
	switch choice.Type {
	case core.ToolChoiceAuto:
		return anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}, true
	case core.ToolChoiceAny:
		return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}, true
	case core.ToolChoiceNone:
		none := anthropic.NewToolChoiceNoneParam()
		return anthropic.ToolChoiceUnionParam{OfNone: &none}, true
	case core.ToolChoiceTool:
		if strings.TrimSpace(choice.Name) == "" {
			return anthropic.ToolChoiceUnionParam{}, false
		}
		return anthropic.ToolChoiceParamOfTool(choice.Name), true
	default:
		return anthropic.ToolChoiceUnionParam{}, false
	}
}
