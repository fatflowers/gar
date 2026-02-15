package core

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestNewToolSpecFromStruct validates happy-path schema reflection for a struct input.
func TestNewToolSpecFromStruct(t *testing.T) {
	type input struct {
		Path string `json:"path" jsonschema:"required"`
	}

	spec, err := NewToolSpecFromStruct("Read", "Read file", input{})
	if err != nil {
		t.Fatalf("NewToolSpecFromStruct() error = %v", err)
	}
	if spec.Name != "Read" {
		t.Fatalf("name mismatch: got %q want %q", spec.Name, "Read")
	}
	if !json.Valid(spec.Schema) {
		t.Fatalf("schema is not valid json: %s", string(spec.Schema))
	}
}

// TestNewToolSpecFromStructRejectsNonStruct guards against unsupported schema input types.
func TestNewToolSpecFromStructRejectsNonStruct(t *testing.T) {
	if _, err := NewToolSpecFromStruct("Read", "Read file", 42); err == nil {
		t.Fatalf("expected error for non-struct schema input")
	}
}

func TestDecodeJSONObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     json.RawMessage
		want    map[string]any
		wantErr bool
		errIs   error
	}{
		{
			name: "empty",
			raw:  json.RawMessage("  "),
			want: map[string]any{},
		},
		{
			name: "valid object",
			raw:  json.RawMessage(`{"path":"main.go","head":10}`),
			want: map[string]any{"path": "main.go", "head": float64(10)},
		},
		{
			name:    "invalid json",
			raw:     json.RawMessage("{"),
			wantErr: true,
			errIs:   ErrInvalidRequest,
		},
		{
			name:    "non-object json",
			raw:     json.RawMessage(`[1,2,3]`),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeJSONObject(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errIs != nil && !errors.Is(err, tc.errIs) {
					t.Fatalf("expected error to wrap %v, got %v", tc.errIs, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("DecodeJSONObject() error = %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("map size mismatch: got %d want %d, map=%#v", len(got), len(tc.want), got)
			}
			for key, wantValue := range tc.want {
				if got[key] != wantValue {
					t.Fatalf("value mismatch for key %q: got=%v want=%v", key, got[key], wantValue)
				}
			}
		})
	}
}

func TestDecodeJSONObjectOrEmpty(t *testing.T) {
	t.Parallel()

	got := DecodeJSONObjectOrEmpty(json.RawMessage("{"))
	if len(got) != 0 {
		t.Fatalf("expected empty object on invalid json, got %#v", got)
	}

	got = DecodeJSONObjectOrEmpty(json.RawMessage(`{"tool":"Read"}`))
	if got["tool"] != "Read" {
		t.Fatalf("unexpected decoded object: %#v", got)
	}
}
