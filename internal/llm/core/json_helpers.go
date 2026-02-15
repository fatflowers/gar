package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// RawJSONFromString returns a detached raw JSON payload when the input is valid JSON.
func RawJSONFromString(raw string) json.RawMessage {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	payload := []byte(trimmed)
	if !json.Valid(payload) {
		return nil
	}
	return append(json.RawMessage(nil), payload...)
}

// MarshalToolInput serializes tool input and guarantees a non-empty JSON object payload.
func MarshalToolInput(input any) (json.RawMessage, error) {
	if input == nil {
		return json.RawMessage("{}"), nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("tool input is not valid json")
	}
	return raw, nil
}
