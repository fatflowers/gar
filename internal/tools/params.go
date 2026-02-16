package tools

import (
	"bytes"
	"encoding/json"
)

func decodeParams(raw json.RawMessage, target any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	return json.Unmarshal(trimmed, target)
}
