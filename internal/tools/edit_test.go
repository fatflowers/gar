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

	workspace := t.TempDir()
	path := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newEditTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"file.txt","oldText":"world","newText":"gar"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got.Content, "Successfully replaced text") {
		t.Fatalf("Execute().Content = %q, want success message", got.Content)
	}
	if !strings.Contains(string(got.Display.Payload), "+1 hello gar") {
		t.Fatalf("Execute().Display.Payload = %q, want diff payload", string(got.Display.Payload))
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

	workspace := t.TempDir()
	path := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newEditTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"file.txt","oldText":"zzz","newText":"x"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "could not find the exact text") {
		t.Fatalf("Execute() error = %v, want not found error", err)
	}
}

func TestEditToolFailsWhenOldTextNotUnique(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(path, []byte("x\ny\nx\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newEditTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"file.txt","oldText":"x","newText":"z"}`))
	if err == nil || !strings.Contains(err.Error(), "must be unique") {
		t.Fatalf("Execute() error = %v, want unique-match error", err)
	}
}

func TestEditToolSupportsLegacyOldNewFields(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(path, []byte("foo"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newEditTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"file.txt","old":"foo","new":"bar"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestEditToolRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("foo"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := newEditTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+outside+`","oldText":"foo","newText":"bar"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "workspace") {
		t.Fatalf("Execute() error = %v, want workspace restriction error", err)
	}
}
