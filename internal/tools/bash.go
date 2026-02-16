package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	bashToolName          = "bash"
	defaultBashTimeout    = 30 * time.Second
	defaultBashOutputSize = 10_000
)

// BashTool executes shell commands synchronously with timeout and truncation.
type BashTool struct {
	defaultTimeout time.Duration
	maxOutputBytes int
}

// NewBashTool constructs bash tool with sensible defaults.
func NewBashTool() BashTool {
	return BashTool{
		defaultTimeout: defaultBashTimeout,
		maxOutputBytes: defaultBashOutputSize,
	}
}

func (BashTool) Name() string { return bashToolName }

func (BashTool) Description() string {
	return "Run a shell command synchronously with timeout and output truncation."
}

func (BashTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"working_dir":{"type":"string"},"timeout_sec":{"type":"integer","minimum":1}},"required":["command"]}`)
}

func (b BashTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Command    string `json:"command"`
		WorkingDir string `json:"working_dir"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode bash params: %w", err)
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return Result{}, errors.New("command is required")
	}

	timeout := b.defaultTimeout
	if input.TimeoutSec > 0 {
		timeout = time.Duration(input.TimeoutSec) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
	if strings.TrimSpace(input.WorkingDir) != "" {
		cmd.Dir = input.WorkingDir
	}
	out, err := cmd.CombinedOutput()
	content, truncated := truncateOutput(out, b.maxOutputBytes)
	details, _ := json.Marshal(map[string]any{
		"command":     command,
		"working_dir": input.WorkingDir,
		"truncated":   truncated,
	})

	result := Result{
		Content: content,
		Display: DisplayData{
			Type:    "bash_output",
			Payload: details,
		},
	}

	if err == nil {
		return result, nil
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("command timeout after %s", timeout)
	}
	return result, fmt.Errorf("command failed: %w", err)
}

func truncateOutput(raw []byte, limit int) (string, bool) {
	if limit <= 0 || len(raw) <= limit {
		return string(raw), false
	}

	headSize := limit / 2
	tailSize := limit - headSize

	var buf bytes.Buffer
	buf.Write(raw[:headSize])
	buf.WriteString("\n...[truncated]...\n")
	buf.Write(raw[len(raw)-tailSize:])
	return buf.String(), true
}
