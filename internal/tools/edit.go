package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const editToolName = "edit"

// EditTool performs string replacement in an existing file.
type EditTool struct {
	workspaceRoot string
}

// NewEditTool constructs the edit tool.
func NewEditTool() EditTool { return newEditTool("") }

func newEditTool(workspaceRoot string) EditTool {
	return EditTool{workspaceRoot: workspaceRoot}
}

func (EditTool) Name() string { return editToolName }

func (EditTool) Description() string {
	return "Edit a file by replacing exact text. The oldText must match exactly (including whitespace). Use this for precise, surgical edits."
}

func (EditTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Brief description of the edit you're making (shown to user)"},"path":{"type":"string","description":"Path to the file to edit (relative or absolute)"},"oldText":{"type":"string","description":"Exact text to find and replace (must match exactly)"},"newText":{"type":"string","description":"New text to replace the old text with"}},"required":["label","path","oldText","newText"]}`)
}

func (e EditTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Label   string `json:"label"`
		Path    string `json:"path"`
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
		Old     string `json:"old"`
		New     string `json:"new"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode edit params: %w", err)
	}

	pathArg := strings.TrimSpace(input.Path)
	if pathArg == "" {
		return Result{}, errors.New("path is required")
	}

	oldText := input.OldText
	if oldText == "" {
		oldText = input.Old
	}
	newText := input.NewText
	if newText == "" && input.New != "" {
		newText = input.New
	}

	if oldText == "" {
		return Result{}, errors.New("oldText is required")
	}

	path, err := resolveWorkspacePath(e.workspaceRoot, pathArg, false)
	if err != nil {
		return Result{}, fmt.Errorf("resolve edit path: %w", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", pathArg, err)
	}
	content := string(raw)

	if !strings.Contains(content, oldText) {
		return Result{}, fmt.Errorf(
			"Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.",
			pathArg,
		)
	}

	occurrences := strings.Count(content, oldText)
	if occurrences > 1 {
		return Result{}, fmt.Errorf(
			"Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.",
			occurrences,
			pathArg,
		)
	}

	index := strings.Index(content, oldText)
	updated := content[:index] + newText + content[index+len(oldText):]
	if content == updated {
		return Result{}, fmt.Errorf(
			"No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.",
			pathArg,
		)
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode()
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", pathArg, err)
	}

	diff := generateDiffString(content, updated, 4)
	details, _ := json.Marshal(map[string]any{"diff": diff})
	return Result{
		Content: fmt.Sprintf(
			"Successfully replaced text in %s. Changed %d characters to %d characters.",
			pathArg,
			len(oldText),
			len(newText),
		),
		Display: DisplayData{
			Type:    "edit_result",
			Payload: details,
		},
	}, nil
}

type lineDiffPart struct {
	added   bool
	removed bool
	lines   []string
}

func generateDiffString(oldContent, newContent string, contextLines int) string {
	parts := diffLineParts(oldContent, newContent)
	output := make([]string, 0, len(parts)*2)

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	lineNumWidth := len(strconv.Itoa(max(len(oldLines), len(newLines))))

	oldLineNum := 1
	newLineNum := 1
	lastWasChange := false

	for i, part := range parts {
		raw := append([]string(nil), part.lines...)
		if len(raw) > 0 && raw[len(raw)-1] == "" {
			raw = raw[:len(raw)-1]
		}

		if part.added || part.removed {
			for _, line := range raw {
				if part.added {
					lineNum := leftPadNumber(newLineNum, lineNumWidth)
					output = append(output, fmt.Sprintf("+%s %s", lineNum, line))
					newLineNum++
				} else {
					lineNum := leftPadNumber(oldLineNum, lineNumWidth)
					output = append(output, fmt.Sprintf("-%s %s", lineNum, line))
					oldLineNum++
				}
			}
			lastWasChange = true
			continue
		}

		nextPartIsChange := i < len(parts)-1 && (parts[i+1].added || parts[i+1].removed)
		if lastWasChange || nextPartIsChange {
			linesToShow := raw
			skipStart := 0
			skipEnd := 0

			if !lastWasChange {
				skipStart = max(0, len(raw)-contextLines)
				linesToShow = raw[skipStart:]
			}

			if !nextPartIsChange && len(linesToShow) > contextLines {
				skipEnd = len(linesToShow) - contextLines
				linesToShow = linesToShow[:contextLines]
			}

			if skipStart > 0 {
				output = append(output, fmt.Sprintf(" %s ...", strings.Repeat(" ", lineNumWidth)))
			}

			for _, line := range linesToShow {
				lineNum := leftPadNumber(oldLineNum, lineNumWidth)
				output = append(output, fmt.Sprintf(" %s %s", lineNum, line))
				oldLineNum++
				newLineNum++
			}

			if skipEnd > 0 {
				output = append(output, fmt.Sprintf(" %s ...", strings.Repeat(" ", lineNumWidth)))
			}

			oldLineNum += skipStart + skipEnd
			newLineNum += skipStart + skipEnd
		} else {
			oldLineNum += len(raw)
			newLineNum += len(raw)
		}

		lastWasChange = false
	}

	return strings.Join(output, "\n")
}

func diffLineParts(oldContent, newContent string) []lineDiffPart {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	start := 0
	for start < len(oldLines) && start < len(newLines) && oldLines[start] == newLines[start] {
		start++
	}

	oldEnd := len(oldLines) - 1
	newEnd := len(newLines) - 1
	for oldEnd >= start && newEnd >= start && oldLines[oldEnd] == newLines[newEnd] {
		oldEnd--
		newEnd--
	}

	parts := make([]lineDiffPart, 0, 4)
	if start > 0 {
		parts = append(parts, lineDiffPart{
			lines: append([]string(nil), oldLines[:start]...),
		})
	}
	if oldEnd >= start {
		parts = append(parts, lineDiffPart{
			removed: true,
			lines:   append([]string(nil), oldLines[start:oldEnd+1]...),
		})
	}
	if newEnd >= start {
		parts = append(parts, lineDiffPart{
			added: true,
			lines: append([]string(nil), newLines[start:newEnd+1]...),
		})
	}
	if oldEnd+1 < len(oldLines) {
		parts = append(parts, lineDiffPart{
			lines: append([]string(nil), oldLines[oldEnd+1:]...),
		})
	}
	return parts
}

func leftPadNumber(value, width int) string {
	return fmt.Sprintf("%*d", width, value)
}
