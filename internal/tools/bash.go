package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const bashToolName = "bash"

// BashTool executes shell commands synchronously.
type BashTool struct {
	maxOutputLines int
	maxOutputBytes int
}

// NewBashTool constructs bash tool with sensible defaults.
func NewBashTool() BashTool {
	return BashTool{
		maxOutputLines: defaultMaxLines,
		maxOutputBytes: defaultMaxBytes,
	}
}

func (BashTool) Name() string { return bashToolName }

func (BashTool) Description() string {
	return fmt.Sprintf(
		"Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.",
		defaultMaxLines,
		defaultMaxBytes/1024,
	)
}

func (BashTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Brief description of what this command does (shown to user)"},"command":{"type":"string","description":"Bash command to execute"},"timeout":{"type":"number","description":"Timeout in seconds (optional, no default timeout)"}},"required":["label","command"]}`)
}

func (b BashTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	var input struct {
		Label      string `json:"label"`
		Command    string `json:"command"`
		Timeout    *int   `json:"timeout"`
		TimeoutSec *int   `json:"timeout_sec"`
	}
	if err := decodeParams(params, &input); err != nil {
		return Result{}, fmt.Errorf("decode bash params: %w", err)
	}

	command := strings.TrimSpace(input.Command)
	if command == "" {
		return Result{}, errors.New("command is required")
	}

	timeoutSeconds := 0
	if input.Timeout != nil {
		timeoutSeconds = *input.Timeout
	} else if input.TimeoutSec != nil {
		timeoutSeconds = *input.TimeoutSec
	}
	if timeoutSeconds < 0 {
		return Result{}, errors.New("timeout must be >= 0")
	}

	runCtx := ctx
	cancel := func() {}
	if timeoutSeconds > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	}
	defer cancel()

	cmd := shellCommand(runCtx, command)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	output := combineStdoutStderr(stdout.String(), stderr.String())

	truncation := truncateTail(output, truncationOptions{MaxLines: b.maxOutputLines, MaxBytes: b.maxOutputBytes})
	outputText := truncation.Content
	if outputText == "" {
		outputText = "(no output)"
	}

	detailsPayload := map[string]any{}
	if truncation.Truncated {
		detailsPayload["truncation"] = truncation

		if fullOutputPath, err := writeFullOutputToTempFile(output); err == nil {
			detailsPayload["full_output_path"] = fullOutputPath

			startLine := truncation.TotalLines - truncation.OutputLines + 1
			endLine := truncation.TotalLines
			if truncation.LastLinePartial {
				lastLine := ""
				lines := strings.Split(output, "\n")
				if len(lines) > 0 {
					lastLine = lines[len(lines)-1]
				}
				outputText += fmt.Sprintf(
					"\n\n[Showing last %s of line %d (line is %s). Full output: %s]",
					formatSize(truncation.OutputBytes),
					endLine,
					formatSize(len([]byte(lastLine))),
					fullOutputPath,
				)
			} else if truncation.TruncatedBy == "lines" {
				outputText += fmt.Sprintf(
					"\n\n[Showing lines %d-%d of %d. Full output: %s]",
					startLine,
					endLine,
					truncation.TotalLines,
					fullOutputPath,
				)
			} else {
				outputText += fmt.Sprintf(
					"\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]",
					startLine,
					endLine,
					truncation.TotalLines,
					formatSize(b.maxOutputBytes),
					fullOutputPath,
				)
			}
		}
	}

	details, _ := json.Marshal(detailsPayload)
	result := Result{
		Content: outputText,
		Display: DisplayData{
			Type:    "bash_output",
			Payload: details,
		},
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return result, errors.New(strings.TrimSpace(outputText + fmt.Sprintf("\n\nCommand timed out after %d seconds", timeoutSeconds)))
	}

	if runErr != nil {
		exitCode := 1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return result, errors.New(strings.TrimSpace(outputText + fmt.Sprintf("\n\nCommand exited with code %d", exitCode)))
	}

	return result, nil
}

func combineStdoutStderr(stdout, stderr string) string {
	if stdout == "" {
		return stderr
	}
	if stderr == "" {
		return stdout
	}
	return stdout + "\n" + stderr
}

func writeFullOutputToTempFile(output string) (string, error) {
	file, err := os.CreateTemp("", "gar-bash-*.log")
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	if _, err := file.WriteString(output); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/c", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}
