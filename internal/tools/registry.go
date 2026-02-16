package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrToolRequired          = errors.New("tool is required")
	ErrToolNameRequired      = errors.New("tool name is required")
	ErrToolAlreadyRegistered = errors.New("tool already registered")
	ErrToolNotFound          = errors.New("tool not found")
)

// DisplayData carries UI-facing structured tool output.
type DisplayData struct {
	Type    string
	Payload json.RawMessage
}

// Result carries tool output split for model and UI channels.
type Result struct {
	Content string
	Display DisplayData
}

// Tool is the canonical runtime contract for all built-in tools.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (Result, error)
}

// Registry stores tools by name and executes them by lookup.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry constructs an empty tool registry and optionally registers tools.
func NewRegistry(initial ...Tool) *Registry {
	r := &Registry{
		tools: make(map[string]Tool, len(initial)),
	}
	for _, tool := range initial {
		_ = r.Register(tool)
	}
	return r
}

// Register inserts a tool by its canonical name.
func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return ErrToolRequired
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return ErrToolNameRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, name)
	}
	r.tools[name] = tool
	return nil
}

// Get returns a registered tool by name.
func (r *Registry) Get(name string) (Tool, error) {
	lookup := strings.TrimSpace(name)
	if lookup == "" {
		return nil, ErrToolNameRequired
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[lookup]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, lookup)
	}
	return tool, nil
}

// Execute resolves a named tool and runs it with provided raw JSON params.
func (r *Registry) Execute(ctx context.Context, name string, params json.RawMessage) (Result, error) {
	tool, err := r.Get(name)
	if err != nil {
		return Result{}, err
	}
	return tool.Execute(ctx, params)
}
