package tools

import (
	"context"
	"encoding/json"
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
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 2","timeout_sec":1}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("Execute() error = %v, want timeout error", err)
	}
}
