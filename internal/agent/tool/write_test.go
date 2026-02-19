package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteToolWritesFileAndCreatesParent(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "nested", "out.txt")

	tool := newWriteTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"nested/out.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got.Content, "Successfully wrote 5 bytes") {
		t.Fatalf("Execute().Content = %q, want success message", got.Content)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(raw) != "hello" {
		t.Fatalf("written content = %q, want hello", string(raw))
	}
}

func TestWriteToolRequiresPath(t *testing.T) {
	t.Parallel()

	tool := newWriteTool(t.TempDir())
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"content":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("Execute() error = %v, want path validation error", err)
	}
}

func TestWriteToolRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")

	tool := newWriteTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+outside+`","content":"x"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "workspace") {
		t.Fatalf("Execute() error = %v, want workspace restriction error", err)
	}
}
