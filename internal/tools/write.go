package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const writeToolName = "write"

// WriteTool writes whole-file content to disk.
type WriteTool struct{}

// NewWriteTool constructs the write tool.
func NewWriteTool() WriteTool { return WriteTool{} }

func (WriteTool) Name() string { return writeToolName }

func (WriteTool) Description() string {
	return "Write full file content to disk, creating parent directories when needed."
}

func (WriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}

func (WriteTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode write params: %w", err)
	}

	path := strings.TrimSpace(input.Path)
	if path == "" {
		return Result{}, errors.New("path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir parent for %s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(input.Content), 0o644); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", path, err)
	}

	msg := fmt.Sprintf("Wrote %d bytes to %s", len(input.Content), path)
	details, _ := json.Marshal(map[string]any{
		"path":  path,
		"bytes": len(input.Content),
	})
	return Result{
		Content: msg,
		Display: DisplayData{
			Type:    "write_result",
			Payload: details,
		},
	}, nil
}
