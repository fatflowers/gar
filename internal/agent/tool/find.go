package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	findToolName       = "find"
	defaultFindLimit   = 1000
	findDisplayTypeKey = "find_result"
)

var errFindLimitReached = errors.New("find limit reached")

// FindTool finds files by glob pattern.
type FindTool struct {
	workspaceRoot string
}

// NewFindTool constructs find tool.
func NewFindTool() FindTool { return newFindTool("") }

func newFindTool(workspaceRoot string) FindTool {
	return FindTool{workspaceRoot: workspaceRoot}
}

func (FindTool) Name() string { return findToolName }

func (FindTool) Description() string {
	return fmt.Sprintf(
		"Search for files by glob pattern. Returns matching file paths relative to the search directory. Respects common ignore folders. Output is truncated to %d results or %dKB (whichever is hit first).",
		defaultFindLimit,
		defaultMaxBytes/1024,
	)
}

func (FindTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Brief description of what you're searching for (shown to user)"},"pattern":{"type":"string","description":"Glob pattern to match files, e.g. '*.ts', '**/*.json', or 'src/**/*.spec.ts'"},"path":{"type":"string","description":"Directory to search in (default: current directory)"},"limit":{"type":"number","description":"Maximum number of results (default: 1000)"}},"required":["pattern"]}`)
}

func (f FindTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Label   string `json:"label"`
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Limit   *int   `json:"limit"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode find params: %w", err)
	}

	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return Result{}, errors.New("pattern is required")
	}

	pathArg := strings.TrimSpace(input.Path)
	if pathArg == "" {
		pathArg = "."
	}

	effectiveLimit := defaultFindLimit
	if input.Limit != nil {
		if *input.Limit <= 0 {
			return Result{}, errors.New("limit must be > 0")
		}
		effectiveLimit = *input.Limit
	}

	searchPath, err := resolveWorkspacePath(f.workspaceRoot, pathArg, false)
	if err != nil {
		return Result{}, fmt.Errorf("resolve find path: %w", err)
	}

	stat, err := os.Stat(searchPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat %s: %w", pathArg, err)
	}
	if !stat.IsDir() {
		return Result{}, fmt.Errorf("not a directory: %s", pathArg)
	}

	results := make([]string, 0, min(effectiveLimit, 128))
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

		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(searchPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		display := rel
		if d.IsDir() {
			display += "/"
		}

		if !matchesGlobPattern(pattern, rel) && !matchesGlobPattern(pattern, display) {
			return nil
		}

		results = append(results, display)
		if len(results) >= effectiveLimit {
			return errFindLimitReached
		}
		return nil
	})

	resultLimitReached := false
	if errors.Is(walkErr, errFindLimitReached) {
		resultLimitReached = true
	} else if walkErr != nil {
		return Result{}, fmt.Errorf("find walk: %w", walkErr)
	}

	if len(results) == 0 {
		return Result{
			Content: "No files found matching pattern",
			Display: DisplayData{Type: findDisplayTypeKey},
		}, nil
	}

	rawOutput := strings.Join(results, "\n")
	truncation := truncateHead(rawOutput, truncationOptions{MaxLines: maxIntValue, MaxBytes: defaultMaxBytes})
	output := truncation.Content

	detailsPayload := map[string]any{}
	notices := make([]string, 0, 2)

	if resultLimitReached {
		notices = append(notices, fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
		detailsPayload["result_limit_reached"] = effectiveLimit
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
			Type:    findDisplayTypeKey,
			Payload: details,
		},
	}, nil
}
