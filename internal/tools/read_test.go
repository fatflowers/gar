package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestReadToolReadsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/main.go"
	content := "package main\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := NewReadTool()
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Content != content {
		t.Fatalf("Execute().Content = %q, want %q", got.Content, content)
	}
}

func TestReadToolRequiresPath(t *testing.T) {
	t.Parallel()

	tool := NewReadTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("Execute() error = %v, want path validation error", err)
	}
}
