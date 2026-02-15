package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
)

var toolSchemaReflector = jsonschema.Reflector{
	DoNotReference:            true,
	AllowAdditionalProperties: false,
}

// toolJSONSchema is the local shape used to normalize reflected JSON Schema payloads.
type toolJSONSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required"`
}

// ToolJSONSchema is the normalized object-schema used by tool mappings.
type ToolJSONSchema = toolJSONSchema

// NewToolSpecFromStruct creates a ToolSpec by reflecting a Go struct into JSON Schema.
func NewToolSpecFromStruct(name, description string, schemaStruct any) (ToolSpec, error) {
	schema, err := buildToolSchemaFromStruct(schemaStruct)
	if err != nil {
		return ToolSpec{}, err
	}
	return ToolSpec{
		Name:        name,
		Description: description,
		Schema:      schema,
	}, nil
}

// buildToolSchemaFromStruct reflects and normalizes a struct schema into raw JSON.
func buildToolSchemaFromStruct(schemaStruct any) (json.RawMessage, error) {
	target, err := schemaReflectionTarget(schemaStruct)
	if err != nil {
		return nil, err
	}

	schema := toolSchemaReflector.Reflect(target)
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal generated tool schema: %w", err)
	}

	decoded, err := DecodeToolJSONSchema(raw)
	if err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized tool schema: %w", err)
	}
	return normalized, nil
}

// schemaReflectionTarget validates schemaStruct and returns a concrete struct pointer.
func schemaReflectionTarget(schemaStruct any) (any, error) {
	t := reflect.TypeOf(schemaStruct)
	if t == nil {
		return nil, fmt.Errorf("%w: schema struct is nil", ErrInvalidRequest)
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: schema struct must be a struct or pointer to struct", ErrInvalidRequest)
	}
	return reflect.New(t).Interface(), nil
}

// DecodeToolJSONSchema validates and normalizes a tool schema JSON object.
func DecodeToolJSONSchema(raw json.RawMessage) (toolJSONSchema, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return toolJSONSchema{
			Type:       "object",
			Properties: map[string]any{},
		}, nil
	}

	var schema toolJSONSchema
	if err := json.Unmarshal(trimmed, &schema); err != nil {
		return toolJSONSchema{}, fmt.Errorf("%w: invalid tool schema json", ErrInvalidRequest)
	}

	if strings.TrimSpace(schema.Type) == "" {
		schema.Type = "object"
	}
	if schema.Type != "object" {
		return toolJSONSchema{}, fmt.Errorf("%w: tool schema type must be object", ErrInvalidRequest)
	}
	if schema.Properties == nil {
		schema.Properties = map[string]any{}
	}

	return schema, nil
}

// DecodeJSONObject validates and decodes a JSON object into a map.
func DecodeJSONObject(raw json.RawMessage) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return map[string]any{}, nil
	}
	if !json.Valid(trimmed) {
		return nil, fmt.Errorf("%w: invalid tool input json", ErrInvalidRequest)
	}
	obj := map[string]any{}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("decode tool input: %w", err)
	}
	return obj, nil
}

// DecodeJSONObjectOrEmpty decodes JSON object input and falls back to an empty map on error.
func DecodeJSONObjectOrEmpty(raw json.RawMessage) map[string]any {
	obj, err := DecodeJSONObject(raw)
	if err != nil {
		return map[string]any{}
	}
	return obj
}
