package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	ErrEditOldTextNotFound  = errors.New("old text not found in file")
	ErrEditOldTextNotUnique = errors.New("old text matched multiple times; set replace_all to true")
)

const editToolName = "edit"

// EditTool performs string replacement in an existing file.
type EditTool struct{}

// NewEditTool constructs the edit tool.
func NewEditTool() EditTool { return EditTool{} }

func (EditTool) Name() string { return editToolName }

func (EditTool) Description() string {
	return "Replace text in an existing file using str_replace semantics."
}

func (EditTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["path","old","new"]}`)
}

func (EditTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode edit params: %w", err)
	}
	if strings.TrimSpace(input.Path) == "" {
		return Result{}, errors.New("path is required")
	}
	if input.Old == "" {
		return Result{}, errors.New("old is required")
	}

	raw, err := os.ReadFile(input.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", input.Path, err)
	}

	original := string(raw)
	occurrences := strings.Count(original, input.Old)
	if occurrences == 0 {
		return Result{}, ErrEditOldTextNotFound
	}
	if occurrences > 1 && !input.ReplaceAll {
		return Result{}, ErrEditOldTextNotUnique
	}

	limit := 1
	if input.ReplaceAll {
		limit = -1
	}
	updated := strings.Replace(original, input.Old, input.New, limit)

	info, statErr := os.Stat(input.Path)
	mode := os.FileMode(0o644)
	if statErr == nil {
		mode = info.Mode()
	}
	if err := os.WriteFile(input.Path, []byte(updated), mode); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", input.Path, err)
	}

	replaced := 1
	if input.ReplaceAll {
		replaced = occurrences
	}
	msg := fmt.Sprintf("Replaced %d occurrence(s) in %s", replaced, input.Path)
	details, _ := json.Marshal(map[string]any{
		"path":       input.Path,
		"replaced":   replaced,
		"replaceAll": input.ReplaceAll,
	})
	return Result{
		Content: msg,
		Display: DisplayData{
			Type:    "edit_result",
			Payload: details,
		},
	}, nil
}
