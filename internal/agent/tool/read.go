package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const readToolName = "read"

// ReadTool reads file contents from disk.
type ReadTool struct {
	workspaceRoot string
	maxLines      int
	maxBytes      int
}

// NewReadTool constructs the read tool.
func NewReadTool() ReadTool { return newReadTool("") }

func newReadTool(workspaceRoot string) ReadTool {
	return ReadTool{
		workspaceRoot: workspaceRoot,
		maxLines:      defaultMaxLines,
		maxBytes:      defaultMaxBytes,
	}
}

func (ReadTool) Name() string { return readToolName }

func (ReadTool) Description() string {
	return fmt.Sprintf(
		"Read the contents of a file. Supports text files and images (jpg, png, gif, webp). For text files, output is truncated to %d lines or %dKB (whichever is hit first). Use offset/limit for large files.",
		defaultMaxLines,
		defaultMaxBytes/1024,
	)
}

func (ReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Brief description of what you're reading and why (shown to user)"},"path":{"type":"string","description":"Path to the file to read (relative or absolute)"},"offset":{"type":"number","description":"Line number to start reading from (1-indexed)"},"limit":{"type":"number","description":"Maximum number of lines to read"}},"required":["label","path"]}`)
}

func (r ReadTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Label  string `json:"label"`
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode read params: %w", err)
	}

	pathArg := strings.TrimSpace(input.Path)
	if pathArg == "" {
		return Result{}, errors.New("path is required")
	}

	path, err := resolveWorkspacePath(r.workspaceRoot, pathArg, false)
	if err != nil {
		return Result{}, fmt.Errorf("resolve read path: %w", err)
	}

	if mimeType, ok := imageMimeTypes[strings.ToLower(filepath.Ext(path))]; ok {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Result{}, fmt.Errorf("read image %s: %w", pathArg, err)
		}

		details, _ := json.Marshal(map[string]any{
			"path":         pathArg,
			"bytes":        len(raw),
			"mime_type":    mimeType,
			"image_base64": base64.StdEncoding.EncodeToString(raw),
		})
		return Result{
			Content: fmt.Sprintf("Read image file [%s]", mimeType),
			Display: DisplayData{
				Type:    "file_content",
				Payload: details,
			},
		}, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", pathArg, err)
	}

	allContent := string(raw)
	allLines := strings.Split(allContent, "\n")
	totalFileLines := len(allLines)

	startLine := 1
	if input.Offset != nil {
		startLine = max(1, *input.Offset)
	}
	if startLine > totalFileLines {
		return Result{}, fmt.Errorf("Offset %d is beyond end of file (%d lines total)", startLine, totalFileLines)
	}

	selectedContent := strings.Join(allLines[startLine-1:], "\n")
	userLimitedLines := -1
	if input.Limit != nil {
		if *input.Limit < 0 {
			return Result{}, errors.New("limit must be >= 0")
		}

		lines := strings.Split(selectedContent, "\n")
		endLine := min(*input.Limit, len(lines))
		selectedContent = strings.Join(lines[:endLine], "\n")
		userLimitedLines = endLine
	}

	truncation := truncateHead(selectedContent, truncationOptions{
		MaxLines: r.maxLines,
		MaxBytes: r.maxBytes,
	})

	var outputText string
	detailsPayload := map[string]any{
		"path": pathArg,
	}

	if truncation.FirstLineExceedsLimit {
		firstLine := ""
		selectedLines := strings.Split(selectedContent, "\n")
		if len(selectedLines) > 0 {
			firstLine = selectedLines[0]
		}

		outputText = fmt.Sprintf(
			"[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startLine,
			formatSize(len([]byte(firstLine))),
			formatSize(r.maxBytes),
			startLine,
			pathArg,
			r.maxBytes,
		)
		detailsPayload["truncation"] = truncation
	} else if truncation.Truncated {
		endLineDisplay := startLine + truncation.OutputLines - 1
		nextOffset := endLineDisplay + 1

		outputText = truncation.Content
		if truncation.TruncatedBy == "lines" {
			outputText += fmt.Sprintf(
				"\n\n[Showing lines %d-%d of %d. Use offset=%d to continue]",
				startLine,
				endLineDisplay,
				totalFileLines,
				nextOffset,
			)
		} else {
			outputText += fmt.Sprintf(
				"\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue]",
				startLine,
				endLineDisplay,
				totalFileLines,
				formatSize(r.maxBytes),
				nextOffset,
			)
		}
		detailsPayload["truncation"] = truncation
	} else if userLimitedLines >= 0 {
		linesFromStart := startLine - 1 + userLimitedLines
		outputText = truncation.Content
		if linesFromStart < totalFileLines {
			remaining := totalFileLines - linesFromStart
			nextOffset := startLine + userLimitedLines
			outputText += fmt.Sprintf(
				"\n\n[%d more lines in file. Use offset=%d to continue]",
				remaining,
				nextOffset,
			)
		}
	} else {
		outputText = truncation.Content
	}

	details, _ := json.Marshal(detailsPayload)
	return Result{
		Content: outputText,
		Display: DisplayData{
			Type:    "file_content",
			Payload: details,
		},
	}, nil
}

var imageMimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}
