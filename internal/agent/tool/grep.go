package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	grepToolName       = "grep"
	defaultGrepLimit   = 100
	grepDisplayTypeKey = "grep_result"
)

type grepMatch struct {
	File string
	Line int
}

// GrepTool searches file content by pattern.
type GrepTool struct {
	workspaceRoot string
}

// NewGrepTool constructs grep tool.
func NewGrepTool() GrepTool { return newGrepTool("") }

func newGrepTool(workspaceRoot string) GrepTool {
	return GrepTool{workspaceRoot: workspaceRoot}
}

func (GrepTool) Name() string { return grepToolName }

func (GrepTool) Description() string {
	return fmt.Sprintf(
		"Search file contents for a pattern. Returns matching lines with file paths and line numbers. Output is truncated to %d matches or %dKB (whichever is hit first). Long lines are truncated to %d chars.",
		defaultGrepLimit,
		defaultMaxBytes/1024,
		grepMaxLineLen,
	)
}

func (GrepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Brief description of what you're searching for (shown to user)"},"pattern":{"type":"string","description":"Search pattern (regex or literal string)"},"path":{"type":"string","description":"Directory or file to search (default: current directory)"},"glob":{"type":"string","description":"Filter files by glob pattern, e.g. '*.ts' or '**/*.spec.ts'"},"ignoreCase":{"type":"boolean","description":"Case-insensitive search (default: false)"},"literal":{"type":"boolean","description":"Treat pattern as literal string instead of regex (default: false)"},"context":{"type":"number","description":"Number of lines to show before and after each match (default: 0)"},"limit":{"type":"number","description":"Maximum number of matches to return (default: 100)"}},"required":["pattern"]}`)
}

func (g GrepTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Label      string `json:"label"`
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignoreCase"`
		Literal    bool   `json:"literal"`
		Context    *int   `json:"context"`
		Limit      *int   `json:"limit"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode grep params: %w", err)
	}

	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return Result{}, errors.New("pattern is required")
	}

	pathArg := strings.TrimSpace(input.Path)
	if pathArg == "" {
		pathArg = "."
	}

	contextLines := 0
	if input.Context != nil {
		if *input.Context < 0 {
			return Result{}, errors.New("context must be >= 0")
		}
		contextLines = *input.Context
	}

	effectiveLimit := defaultGrepLimit
	if input.Limit != nil {
		if *input.Limit <= 0 {
			return Result{}, errors.New("limit must be > 0")
		}
		effectiveLimit = *input.Limit
	}

	searchPath, err := resolveWorkspacePath(g.workspaceRoot, pathArg, false)
	if err != nil {
		return Result{}, fmt.Errorf("resolve grep path: %w", err)
	}

	searchInfo, err := os.Stat(searchPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat %s: %w", pathArg, err)
	}
	searchIsDir := searchInfo.IsDir()

	patternExpr := pattern
	if input.Literal {
		patternExpr = regexp.QuoteMeta(patternExpr)
	}
	if input.IgnoreCase {
		patternExpr = "(?i)" + patternExpr
	}

	re, err := regexp.Compile(patternExpr)
	if err != nil {
		return Result{}, fmt.Errorf("invalid pattern: %w", err)
	}

	files, err := collectGrepFiles(ctx, searchPath, searchIsDir)
	if err != nil {
		return Result{}, err
	}

	matches := make([]grepMatch, 0, min(effectiveLimit, 64))
	fileLines := make(map[string][]string, len(files))

matchLoop:
	for _, file := range files {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		default:
		}

		relative := filepath.Base(file)
		if searchIsDir {
			rel, relErr := filepath.Rel(searchPath, file)
			if relErr == nil {
				relative = filepath.ToSlash(rel)
			}
		}
		if glob := strings.TrimSpace(input.Glob); glob != "" && !matchesGlobPattern(glob, relative) {
			continue
		}

		raw, readErr := os.ReadFile(file)
		if readErr != nil {
			continue
		}
		lines := strings.Split(normalizeToLF(string(raw)), "\n")
		fileLines[file] = lines

		for idx, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			matches = append(matches, grepMatch{File: file, Line: idx + 1})
			if len(matches) >= effectiveLimit {
				break matchLoop
			}
		}
	}

	if len(matches) == 0 {
		return Result{
			Content: "No matches found",
			Display: DisplayData{Type: grepDisplayTypeKey},
		}, nil
	}

	linesOut := make([]string, 0, len(matches)*(1+2*contextLines))
	linesTruncated := false
	for _, match := range matches {
		lines := fileLines[match.File]
		if len(lines) == 0 {
			continue
		}

		pathDisplay := filepath.Base(match.File)
		if searchIsDir {
			if rel, relErr := filepath.Rel(searchPath, match.File); relErr == nil {
				pathDisplay = filepath.ToSlash(rel)
			}
		}

		start := match.Line
		end := match.Line
		if contextLines > 0 {
			start = max(1, match.Line-contextLines)
			end = min(len(lines), match.Line+contextLines)
		}

		for lineNumber := start; lineNumber <= end; lineNumber++ {
			original := strings.ReplaceAll(lines[lineNumber-1], "\r", "")
			trimmed, wasTruncated := truncateLine(original, grepMaxLineLen)
			if wasTruncated {
				linesTruncated = true
			}

			if lineNumber == match.Line {
				linesOut = append(linesOut, fmt.Sprintf("%s:%d: %s", pathDisplay, lineNumber, trimmed))
			} else {
				linesOut = append(linesOut, fmt.Sprintf("%s-%d- %s", pathDisplay, lineNumber, trimmed))
			}
		}
	}

	rawOutput := strings.Join(linesOut, "\n")
	truncation := truncateHead(rawOutput, truncationOptions{MaxLines: maxIntValue, MaxBytes: defaultMaxBytes})
	output := truncation.Content

	detailsPayload := map[string]any{}
	notices := make([]string, 0, 3)

	if len(matches) >= effectiveLimit {
		notices = append(notices, fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
		detailsPayload["match_limit_reached"] = effectiveLimit
	}
	if truncation.Truncated {
		notices = append(notices, fmt.Sprintf("%s limit reached", formatSize(defaultMaxBytes)))
		detailsPayload["truncation"] = truncation
	}
	if linesTruncated {
		notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", grepMaxLineLen))
		detailsPayload["lines_truncated"] = true
	}
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}

	details, _ := json.Marshal(detailsPayload)
	return Result{
		Content: output,
		Display: DisplayData{
			Type:    grepDisplayTypeKey,
			Payload: details,
		},
	}, nil
}

func collectGrepFiles(ctx context.Context, searchPath string, searchIsDir bool) ([]string, error) {
	if !searchIsDir {
		return []string{searchPath}, nil
	}

	files := make([]string, 0, 256)
	walkErr := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if path == searchPath {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("grep walk: %w", walkErr)
	}
	return files, nil
}
