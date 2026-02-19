package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLsToolListsDirectorySortedWithSuffix(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "dir"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	tool := newLsTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(got.Content), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3; content=%q", len(lines), got.Content)
	}
	if lines[0] != "a.txt" || lines[1] != "b.txt" || lines[2] != "dir/" {
		t.Fatalf("lines = %#v, want [a.txt b.txt dir/]", lines)
	}
}

func TestLsToolHonorsLimit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newLsTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":".","limit":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got.Content, "1 entries limit reached") {
		t.Fatalf("Execute().Content = %q, want entries limit notice", got.Content)
	}
}

func TestLsToolRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()

	tool := newLsTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+outside+`"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "workspace") {
		t.Fatalf("Execute() error = %v, want workspace restriction error", err)
	}
}
