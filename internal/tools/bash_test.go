package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashToolRunsCommand(t *testing.T) {
	t.Parallel()

	tool := NewBashTool()
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"printf 'ok'"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(got.Content) != "ok" {
		t.Fatalf("Execute().Content = %q, want ok", got.Content)
	}
}

func TestBashToolHonorsTimeout(t *testing.T) {
	t.Parallel()

	tool := NewBashTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 2","timeout":1}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "timed out") {
		t.Fatalf("Execute() error = %v, want timeout error", err)
	}
}

func TestBashToolSupportsLegacyTimeoutSec(t *testing.T) {
	t.Parallel()

	tool := NewBashTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 2","timeout_sec":1}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "timed out") {
		t.Fatalf("Execute() error = %v, want timeout error", err)
	}
}

func TestBashToolReturnsExitCodeWithOutputOnFailure(t *testing.T) {
	t.Parallel()

	tool := NewBashTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"printf 'boom'; exit 7"}`))
	if err == nil {
		t.Fatalf("Execute() error = nil, want command failure")
	}

	msg := err.Error()
	if !strings.Contains(msg, "boom") || !strings.Contains(msg, "Command exited with code 7") {
		t.Fatalf("Execute() error = %q, want output and exit code", msg)
	}
}

func TestBashToolTruncatesOutputAndPersistsFullOutput(t *testing.T) {
	t.Parallel()

	tool := BashTool{
		maxOutputLines: 3,
		maxOutputBytes: defaultMaxBytes,
	}

	got, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"printf '1\n2\n3\n4\n5\n'"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(got.Content, "[Showing lines") {
		t.Fatalf("Execute().Content = %q, want truncation notice", got.Content)
	}

	payload := string(got.Display.Payload)
	if !strings.Contains(payload, "full_output_path") {
		t.Fatalf("Execute().Display.Payload = %q, want full_output_path", payload)
	}

	// Ensure temp file path exists when returned.
	type display struct {
		FullOutputPath string `json:"full_output_path"`
	}
	var d display
	if err := json.Unmarshal(got.Display.Payload, &d); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if strings.TrimSpace(d.FullOutputPath) == "" {
		t.Fatalf("full_output_path empty in payload %q", payload)
	}
	if _, err := os.Stat(filepath.Clean(d.FullOutputPath)); err != nil {
		t.Fatalf("os.Stat(%q) error = %v", d.FullOutputPath, err)
	}
}
