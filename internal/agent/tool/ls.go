package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	lsToolName       = "ls"
	defaultLsLimit   = 500
	maxIntValue      = int(^uint(0) >> 1)
	lsDisplayTypeKey = "ls_result"
)

// LsTool lists directory contents.
type LsTool struct {
	workspaceRoot string
}

// NewLsTool constructs ls tool.
func NewLsTool() LsTool { return newLsTool("") }

func newLsTool(workspaceRoot string) LsTool {
	return LsTool{workspaceRoot: workspaceRoot}
}

func (LsTool) Name() string { return lsToolName }

func (LsTool) Description() string {
	return fmt.Sprintf(
		"List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. Output is truncated to %d entries or %dKB (whichever is hit first).",
		defaultLsLimit,
		defaultMaxBytes/1024,
	)
}

func (LsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Brief description of what you're listing (shown to user)"},"path":{"type":"string","description":"Directory to list (default: current directory)"},"limit":{"type":"number","description":"Maximum number of entries to return (default: 500)"}}}`)
}

func (l LsTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Label string `json:"label"`
		Path  string `json:"path"`
		Limit *int   `json:"limit"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode ls params: %w", err)
	}

	pathArg := strings.TrimSpace(input.Path)
	if pathArg == "" {
		pathArg = "."
	}

	effectiveLimit := defaultLsLimit
	if input.Limit != nil {
		if *input.Limit <= 0 {
			return Result{}, errors.New("limit must be > 0")
		}
		effectiveLimit = *input.Limit
	}

	dirPath, err := resolveWorkspacePath(l.workspaceRoot, pathArg, false)
	if err != nil {
		return Result{}, fmt.Errorf("resolve ls path: %w", err)
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat %s: %w", pathArg, err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("not a directory: %s", pathArg)
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return Result{}, fmt.Errorf("cannot read directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	results := make([]string, 0, min(len(entries), effectiveLimit))
	entryLimitReached := false
	for _, entry := range entries {
		if len(results) >= effectiveLimit {
			entryLimitReached = true
			break
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		results = append(results, name)
	}

	if len(results) == 0 {
		return Result{
			Content: "(empty directory)",
			Display: DisplayData{Type: lsDisplayTypeKey},
		}, nil
	}

	rawOutput := strings.Join(results, "\n")
	truncation := truncateHead(rawOutput, truncationOptions{MaxLines: maxIntValue, MaxBytes: defaultMaxBytes})

	output := truncation.Content
	detailsPayload := map[string]any{}
	notices := make([]string, 0, 2)

	if entryLimitReached {
		notices = append(notices, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", effectiveLimit, effectiveLimit*2))
		detailsPayload["entry_limit_reached"] = effectiveLimit
	}
	if truncation.Truncated {
		notices = append(notices, fmt.Sprintf("%s limit reached", formatSize(defaultMaxBytes)))
		detailsPayload["truncation"] = truncation
	}
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}

	details, _ := json.Marshal(detailsPayload)
	return Result{
		Content: output,
		Display: DisplayData{
			Type:    lsDisplayTypeKey,
			Payload: details,
		},
	}, nil
}
