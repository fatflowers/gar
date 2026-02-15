package anthropicprovider

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"gar/internal/llm/core"
)

type serializedAnthropicParams struct {
	Model       string                       `json:"model"`
	MaxTokens   int64                        `json:"max_tokens"`
	Messages    []serializedAnthropicMessage `json:"messages"`
	Tools       []serializedAnthropicTool    `json:"tools"`
	System      []serializedAnthropicBlock   `json:"system"`
	Temperature float64                      `json:"temperature"`
	Metadata    map[string]any               `json:"metadata"`
	ToolChoice  map[string]any               `json:"tool_choice"`
}

type serializedAnthropicMessage struct {
	Role    string                     `json:"role"`
	Content []serializedAnthropicBlock `json:"content"`
}

type serializedAnthropicBlock struct {
	Type      string                         `json:"type"`
	Text      string                         `json:"text"`
	ID        string                         `json:"id"`
	Name      string                         `json:"name"`
	Input     map[string]any                 `json:"input"`
	ToolUseID string                         `json:"tool_use_id"`
	IsError   bool                           `json:"is_error"`
	Content   []serializedAnthropicTextBlock `json:"content"`
}

type serializedAnthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type serializedAnthropicTool struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description"`
	InputSchema serializedAnthropicToolSchema `json:"input_schema"`
}

type serializedAnthropicToolSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required"`
}

// TestToAnthropicSDKParamsTextOnly verifies text-only canonical requests map to one user message.
func TestToAnthropicSDKParamsTextOnly(t *testing.T) {
	req := &core.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []core.Message{
			{
				Role: core.RoleUser,
				Content: []core.ContentBlock{
					{Type: core.ContentTypeText, Text: "hello"},
				},
			},
		},
		MaxTokens: 512,
	}

	params, err := toAnthropicSDKParams(req)
	if err != nil {
		t.Fatalf("toAnthropicSDKParams() error = %v", err)
	}

	body := decodeSDKParams(t, params)
	if body.Model != req.Model {
		t.Fatalf("model mismatch: got %q want %q", body.Model, req.Model)
	}
	if body.MaxTokens != 512 {
		t.Fatalf("max_tokens mismatch: got %d want %d", body.MaxTokens, 512)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("message count mismatch: got %d want 1", len(body.Messages))
	}
	got := body.Messages[0]
	if got.Role != "user" {
		t.Fatalf("role mismatch: got %q want %q", got.Role, "user")
	}
	if len(got.Content) != 1 {
		t.Fatalf("content count mismatch: got %d want 1", len(got.Content))
	}
	if got.Content[0].Type != "text" || got.Content[0].Text != "hello" {
		t.Fatalf("unexpected content block: %+v", got.Content[0])
	}
}

func TestToAnthropicSDKParamsPreservesWhitespaceInTextBlocks(t *testing.T) {
	t.Parallel()

	text := "  first line\nsecond line  "
	req := &core.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []core.Message{
			{
				Role: core.RoleUser,
				Content: []core.ContentBlock{
					{Type: core.ContentTypeText, Text: text},
				},
			},
		},
		MaxTokens: 128,
	}

	params, err := toAnthropicSDKParams(req)
	if err != nil {
		t.Fatalf("toAnthropicSDKParams() error = %v", err)
	}

	body := decodeSDKParams(t, params)
	if len(body.Messages) != 1 || len(body.Messages[0].Content) != 1 {
		t.Fatalf("unexpected mapped message/content count: %+v", body.Messages)
	}
	if body.Messages[0].Content[0].Text != text {
		t.Fatalf("mapped text mismatch: got %q want %q", body.Messages[0].Content[0].Text, text)
	}
}

// TestToAnthropicSDKParamsGroupsConsecutiveToolResults ensures adjacent tool results are batched into one user message.
func TestToAnthropicSDKParamsGroupsConsecutiveToolResults(t *testing.T) {
	req := &core.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 128,
		Messages: []core.Message{
			{
				Role: core.RoleTool,
				ToolResult: &core.ToolResult{
					ToolCallID: "tool_1",
					ToolName:   "Read",
					Content:    "file a",
					IsError:    false,
				},
			},
			{
				Role: core.RoleTool,
				ToolResult: &core.ToolResult{
					ToolCallID: "tool_2",
					ToolName:   "Read",
					Content:    "file b",
					IsError:    true,
				},
			},
		},
	}

	params, err := toAnthropicSDKParams(req)
	if err != nil {
		t.Fatalf("toAnthropicSDKParams() error = %v", err)
	}

	body := decodeSDKParams(t, params)
	if len(body.Messages) != 1 {
		t.Fatalf("expected grouped tool results in one user message, got %d", len(body.Messages))
	}
	userMsg := body.Messages[0]
	if userMsg.Role != "user" {
		t.Fatalf("expected user role for tool_result message, got %q", userMsg.Role)
	}
	if len(userMsg.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d", len(userMsg.Content))
	}
	if userMsg.Content[0].Type != "tool_result" || userMsg.Content[1].Type != "tool_result" {
		t.Fatalf("expected tool_result blocks, got %+v", userMsg.Content)
	}
	if userMsg.Content[0].ToolUseID != "tool_1" || userMsg.Content[1].ToolUseID != "tool_2" {
		t.Fatalf("unexpected tool_use_ids: %+v", userMsg.Content)
	}
	if len(userMsg.Content[0].Content) != 1 || userMsg.Content[0].Content[0].Text != "file a" {
		t.Fatalf("unexpected first tool content: %+v", userMsg.Content[0].Content)
	}
	if !userMsg.Content[1].IsError {
		t.Fatalf("expected second tool_result is_error=true")
	}
}

// TestToAnthropicSDKParamsUsesGeneratedToolSchema verifies reflected tool schema is preserved in SDK params.
func TestToAnthropicSDKParamsUsesGeneratedToolSchema(t *testing.T) {
	type readToolInput struct {
		Path string `json:"path" jsonschema:"required"`
		Head int    `json:"head,omitempty"`
	}

	tool, err := core.NewToolSpecFromStruct("Read", "Read file content", readToolInput{})
	if err != nil {
		t.Fatalf("NewToolSpecFromStruct() error = %v", err)
	}

	req := &core.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []core.Message{
			{
				Role: core.RoleUser,
				Content: []core.ContentBlock{
					{Type: core.ContentTypeText, Text: "read main.go"},
				},
			},
		},
		MaxTokens: 128,
		Tools:     []core.ToolSpec{tool},
	}

	params, err := toAnthropicSDKParams(req)
	if err != nil {
		t.Fatalf("toAnthropicSDKParams() error = %v", err)
	}

	body := decodeSDKParams(t, params)
	if len(body.Tools) != 1 {
		t.Fatalf("tool count mismatch: got %d want 1", len(body.Tools))
	}
	got := body.Tools[0]
	if got.Name != "Read" {
		t.Fatalf("tool name mismatch: got %q want %q", got.Name, "Read")
	}
	if got.InputSchema.Type != "object" {
		t.Fatalf("input schema type mismatch: got %q want %q", got.InputSchema.Type, "object")
	}
	if _, ok := got.InputSchema.Properties["path"]; !ok {
		t.Fatalf("expected path property in input schema: %+v", got.InputSchema.Properties)
	}
	if !containsString(got.InputSchema.Required, "path") {
		t.Fatalf("expected required includes path: %+v", got.InputSchema.Required)
	}
}

// decodeSDKParams marshals and decodes SDK params into assertion-friendly structs.
func decodeSDKParams(t *testing.T, params any) serializedAnthropicParams {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var body serializedAnthropicParams
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	return body
}

// containsString reports whether target appears in items.
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// TestMapStopReason verifies Anthropic stop reasons map to canonical stop reasons.
func TestMapStopReason(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   core.StopReason
		hasErr bool
	}{
		{name: "end_turn", input: "end_turn", want: core.StopReasonStop},
		{name: "max_tokens", input: "max_tokens", want: core.StopReasonLength},
		{name: "tool_use", input: "tool_use", want: core.StopReasonToolUse},
		{name: "refusal", input: "refusal", want: core.StopReasonError},
		{name: "unknown", input: "unknown_reason", hasErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := mapStopReason(tc.input)
			if tc.hasErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("mapStopReason() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("mapStopReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToAnthropicSDKParamsRejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	if _, err := toAnthropicSDKParams(nil); !errors.Is(err, core.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for nil request, got %v", err)
	}

	if _, err := toAnthropicSDKParams(&core.Request{Model: "   "}); !errors.Is(err, core.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for missing model, got %v", err)
	}
}

func TestToSDKMessagesRejectsUnsupportedRole(t *testing.T) {
	t.Parallel()

	_, err := toSDKMessages([]core.Message{
		{Role: core.Role("moderator")},
	})
	if !errors.Is(err, core.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for unsupported role, got %v", err)
	}
}

func TestToAnthropicSDKParamsMapsAssistantToolCalls(t *testing.T) {
	t.Parallel()

	params, err := toAnthropicSDKParams(&core.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 128,
		Messages: []core.Message{
			{
				Role: core.RoleAssistant,
				Content: []core.ContentBlock{
					{Type: core.ContentTypeText, Text: "let me read that"},
				},
				ToolCalls: []core.ToolCall{
					{ID: "toolu_1", Name: "Read", Arguments: json.RawMessage(`{"path":"main.go"}`)},
					{ID: "", Name: "Ignored", Arguments: json.RawMessage(`{"path":"skip"}`)},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicSDKParams() error = %v", err)
	}

	body := decodeSDKParams(t, params)
	if len(body.Messages) != 1 {
		t.Fatalf("message count mismatch: got %d want 1", len(body.Messages))
	}
	msg := body.Messages[0]
	if msg.Role != "assistant" {
		t.Fatalf("role mismatch: got %q want assistant", msg.Role)
	}

	var sawText bool
	var sawTool bool
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if block.Text == "let me read that" {
				sawText = true
			}
		case "tool_use":
			sawTool = true
			if block.ID != "toolu_1" || block.Name != "Read" {
				t.Fatalf("unexpected tool_use identity: %+v", block)
			}
			if block.Input["path"] != "main.go" {
				t.Fatalf("unexpected tool_use input: %+v", block.Input)
			}
		}
	}

	if !sawText {
		t.Fatalf("expected assistant text block")
	}
	if !sawTool {
		t.Fatalf("expected assistant tool_use block")
	}
}

func TestToAnthropicSDKParamsMapsOptionalFields(t *testing.T) {
	t.Parallel()

	temp := 0.3
	params, err := toAnthropicSDKParams(&core.Request{
		Model:       "claude-sonnet-4-20250514",
		MaxTokens:   256,
		System:      "You are concise.",
		Temperature: &temp,
		Metadata:    map[string]string{"user_id": "user-123"},
		ToolChoice:  core.ToolChoice{Type: core.ToolChoiceAny},
		Messages: []core.Message{
			{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentTypeText, Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicSDKParams() error = %v", err)
	}

	body := decodeSDKParams(t, params)
	if len(body.System) != 1 || body.System[0].Text != "You are concise." {
		t.Fatalf("unexpected system prompt mapping: %+v", body.System)
	}
	if math.Abs(body.Temperature-temp) > 1e-12 {
		t.Fatalf("temperature mismatch: got %v want %v", body.Temperature, temp)
	}
	if body.Metadata["user_id"] != "user-123" {
		t.Fatalf("metadata user_id mismatch: %+v", body.Metadata)
	}
	if body.ToolChoice["type"] != "any" {
		t.Fatalf("tool choice mismatch: %+v", body.ToolChoice)
	}
}

func TestToSDKToolChoiceMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		choice       core.ToolChoice
		wantOK       bool
		wantContains []string
	}{
		{
			name:         "auto",
			choice:       core.ToolChoice{Type: core.ToolChoiceAuto},
			wantOK:       true,
			wantContains: []string{`"type":"auto"`},
		},
		{
			name:         "any",
			choice:       core.ToolChoice{Type: core.ToolChoiceAny},
			wantOK:       true,
			wantContains: []string{`"type":"any"`},
		},
		{
			name:         "none",
			choice:       core.ToolChoice{Type: core.ToolChoiceNone},
			wantOK:       true,
			wantContains: []string{`"type":"none"`},
		},
		{
			name:         "tool with name",
			choice:       core.ToolChoice{Type: core.ToolChoiceTool, Name: "Read"},
			wantOK:       true,
			wantContains: []string{`"type":"tool"`, `"name":"Read"`},
		},
		{
			name:   "tool without name",
			choice: core.ToolChoice{Type: core.ToolChoiceTool, Name: "  "},
			wantOK: false,
		},
		{
			name:   "unknown",
			choice: core.ToolChoice{Type: core.ToolChoiceType("custom")},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toSDKToolChoice(tc.choice)
			if ok != tc.wantOK {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}

			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal tool choice: %v", err)
			}
			for _, needle := range tc.wantContains {
				if !bytes.Contains(raw, []byte(needle)) {
					t.Fatalf("expected tool choice json to contain %q, got %s", needle, string(raw))
				}
			}
		})
	}
}
