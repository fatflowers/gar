package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeTool struct {
	name string
	run  func(ctx context.Context, params json.RawMessage) (Result, error)
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) Description() string { return "fake tool" }

func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (f fakeTool) Execute(ctx context.Context, params json.RawMessage) (Result, error) {
	if f.run == nil {
		return Result{}, nil
	}
	return f.run(ctx, params)
}

func TestRegistryRegisterGetAndExecute(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	called := false
	tool := fakeTool{
		name: "echo",
		run: func(ctx context.Context, params json.RawMessage) (Result, error) {
			_ = ctx
			called = true
			return Result{Content: string(params)}, nil
		},
	}

	if err := reg.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	gotTool, err := reg.Get("echo")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if gotTool.Name() != "echo" {
		t.Fatalf("Get().Name() = %q, want echo", gotTool.Name())
	}

	got, err := reg.Execute(context.Background(), "echo", json.RawMessage(`{"x":"y"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatalf("tool Execute() was not called")
	}
	if got.Content != `{"x":"y"}` {
		t.Fatalf("Execute().Content = %q, want JSON input echo", got.Content)
	}
}

func TestRegistryRejectsDuplicateName(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	tool := fakeTool{name: "echo"}
	if err := reg.Register(tool); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	err := reg.Register(tool)
	if !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Fatalf("second Register() error = %v, want ErrToolAlreadyRegistered", err)
	}
}

func TestRegistryExecuteUnknownTool(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	_, err := reg.Execute(context.Background(), "missing", nil)
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("Execute() error = %v, want ErrToolNotFound", err)
	}
}
