package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadToolReadsFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	content := "package main\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := newReadTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"main.go"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Content != content {
		t.Fatalf("Execute().Content = %q, want %q", got.Content, content)
	}
}

func TestReadToolSupportsOffsetAndLimit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := newReadTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"file.txt","offset":2,"limit":2}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "b\nc\n\n[1 more lines in file. Use offset=4 to continue]"
	if got.Content != want {
		t.Fatalf("Execute().Content = %q, want %q", got.Content, want)
	}
}

func TestReadToolTruncatesByLineLimit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "long.txt")

	var b strings.Builder
	for i := range defaultMaxLines + 5 {
		fmt.Fprintf(&b, "line-%d\n", i+1)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := newReadTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"long.txt"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	wantNotice := "[Showing lines 1-2000 of 2006. Use offset=2001 to continue]"
	if !strings.Contains(got.Content, wantNotice) {
		t.Fatalf("Execute().Content missing notice %q, got %q", wantNotice, got.Content)
	}
}

func TestReadToolRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := newReadTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"`+outside+`"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "workspace") {
		t.Fatalf("Execute() error = %v, want workspace restriction error", err)
	}
}

func TestReadToolSupportsAtPrefixedPath(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := newReadTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"@main.go"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Content != "package main\n" {
		t.Fatalf("Execute().Content = %q, want file content", got.Content)
	}
}

func TestReadToolRequiresPath(t *testing.T) {
	t.Parallel()

	tool := newReadTool(t.TempDir())
	_, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("Execute() error = %v, want path validation error", err)
	}
}

func TestReadToolRejectsOffsetBeyondEOF(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(path, []byte("x\ny"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := newReadTool(workspace)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"file.txt","offset":3}`))
	if err == nil || !strings.Contains(err.Error(), "beyond end of file") {
		t.Fatalf("Execute() error = %v, want beyond end of file error", err)
	}
}

func TestReadToolFirstLineOverByteLimitSuggestsBash(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "huge.txt")
	line := strings.Repeat("x", defaultMaxBytes+1)
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := newReadTool(workspace)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"huge.txt"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "Use bash: sed -n '1p' huge.txt | head -c " + strconv.Itoa(defaultMaxBytes)
	if !strings.Contains(got.Content, want) {
		t.Fatalf("Execute().Content = %q, want substring %q", got.Content, want)
	}
}
