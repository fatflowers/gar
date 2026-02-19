package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepToolFindsMatchesWithContext(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := strings.Join([]string{
		"line one",
		"error: first",
		"line three",
		"error: second",
		"line five",
	}, "\n")
	if err := os.WriteFile(filepath.Join(workspace, "app.log"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newGrepTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"error","path":".","literal":true,"context":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(got.Content, "app.log:2: error: first") {
		t.Fatalf("Execute().Content = %q, want primary match line", got.Content)
	}
	if !strings.Contains(got.Content, "app.log-1- line one") || !strings.Contains(got.Content, "app.log-3- line three") {
		t.Fatalf("Execute().Content = %q, want context lines", got.Content)
	}
}

func TestGrepToolHonorsLimit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := "error one\nerror two\n"
	if err := os.WriteFile(filepath.Join(workspace, "app.log"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newGrepTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"error","path":".","literal":true,"limit":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got.Content, "1 matches limit reached") {
		t.Fatalf("Execute().Content = %q, want matches limit notice", got.Content)
	}
}

func TestGrepToolSupportsIgnoreCase(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("Fatal ERROR\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newGrepTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"error","ignoreCase":true}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(strings.ToLower(got.Content), "error") {
		t.Fatalf("Execute().Content = %q, want case-insensitive match", got.Content)
	}
}

func TestGrepToolRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()

	tool := newGrepTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"x","path":"`+outside+`"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "workspace") {
		t.Fatalf("Execute() error = %v, want workspace restriction error", err)
	}
}
