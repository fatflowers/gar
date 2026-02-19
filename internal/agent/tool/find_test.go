package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindToolMatchesGlobUnderPath(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "a.go"), []byte("package main"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "b.go"), []byte("package main"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "c.txt"), []byte("text"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newFindTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"*.go","path":"src"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got.Content, "a.go") || !strings.Contains(got.Content, "b.go") {
		t.Fatalf("Execute().Content = %q, want both .go files", got.Content)
	}
	if strings.Contains(got.Content, "c.txt") {
		t.Fatalf("Execute().Content = %q, should not include c.txt", got.Content)
	}
}

func TestFindToolHonorsLimit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newFindTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"*.md","limit":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got.Content, "1 results limit reached") {
		t.Fatalf("Execute().Content = %q, want results limit notice", got.Content)
	}
}

func TestFindToolRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()

	tool := newFindTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"*","path":"`+outside+`"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "workspace") {
		t.Fatalf("Execute() error = %v, want workspace restriction error", err)
	}
}
