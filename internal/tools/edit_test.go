package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditToolReplacesSingleOccurrence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewEditTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","old":"world","new":"gar"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(raw) != "hello gar" {
		t.Fatalf("edited content = %q, want hello gar", string(raw))
	}
}

func TestEditToolFailsWhenOldTextNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewEditTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","old":"zzz","new":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Execute() error = %v, want not found error", err)
	}
}
