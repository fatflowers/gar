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
type WriteTool struct {
	workspaceRoot string
}

// NewWriteTool constructs the write tool.
func NewWriteTool() WriteTool { return newWriteTool("") }

func newWriteTool(workspaceRoot string) WriteTool {
	return WriteTool{workspaceRoot: workspaceRoot}
}

func (WriteTool) Name() string { return writeToolName }

func (WriteTool) Description() string {
	return "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."
}

func (WriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Brief description of what you're writing (shown to user)"},"path":{"type":"string","description":"Path to the file to write (relative or absolute)"},"content":{"type":"string","description":"Content to write to the file"}},"required":["label","path","content"]}`)
}

func (w WriteTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Label   string `json:"label"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode write params: %w", err)
	}

	pathArg := strings.TrimSpace(input.Path)
	if pathArg == "" {
		return Result{}, errors.New("path is required")
	}

	path, err := resolveWorkspacePath(w.workspaceRoot, pathArg, true)
	if err != nil {
		return Result{}, fmt.Errorf("resolve write path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir parent for %s: %w", pathArg, err)
	}
	if err := os.WriteFile(path, []byte(input.Content), 0o644); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", pathArg, err)
	}

	written := len([]byte(input.Content))
	content := fmt.Sprintf("Successfully wrote %d bytes to %s", written, pathArg)
	details, _ := json.Marshal(map[string]any{
		"path":  pathArg,
		"bytes": written,
	})
	return Result{
		Content: content,
		Display: DisplayData{
			Type:    "write_result",
			Payload: details,
		},
	}, nil
}
