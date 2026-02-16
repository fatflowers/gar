package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const readToolName = "read"

// ReadTool reads file contents from disk.
type ReadTool struct{}

// NewReadTool constructs the read tool.
func NewReadTool() ReadTool { return ReadTool{} }

func (ReadTool) Name() string { return readToolName }

func (ReadTool) Description() string {
	return "Read a file from disk by path."
}

func (ReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}

func (ReadTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Path string `json:"path"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode read params: %w", err)
	}

	path := strings.TrimSpace(input.Path)
	if path == "" {
		return Result{}, errors.New("path is required")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", path, err)
	}

	details, _ := json.Marshal(map[string]any{
		"path":  path,
		"bytes": len(raw),
	})
	return Result{
		Content: string(raw),
		Display: DisplayData{
			Type:    "file_content",
			Payload: details,
		},
	}, nil
}
