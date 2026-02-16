package tools

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

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "out.txt")

	tool := NewWriteTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","content":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
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

	tool := NewWriteTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"content":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("Execute() error = %v, want path validation error", err)
	}
}
