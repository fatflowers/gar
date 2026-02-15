package core

import (
	"encoding/json"
	"errors"
	"testing"
)

type invalidJSONMarshaler struct{}

func (invalidJSONMarshaler) MarshalJSON() ([]byte, error) {
	return []byte("{"), nil
}

type errJSONMarshaler struct{}

func (errJSONMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("boom")
}

type emptyJSONMarshaler struct{}

func (emptyJSONMarshaler) MarshalJSON() ([]byte, error) {
	return []byte("{}"), nil
}

func TestRawJSONFromString(t *testing.T) {
	t.Parallel()

	if got := RawJSONFromString("   "); got != nil {
		t.Fatalf("expected nil for empty input, got %q", string(got))
	}
	if got := RawJSONFromString("{"); got != nil {
		t.Fatalf("expected nil for invalid json, got %q", string(got))
	}

	got := RawJSONFromString("  {\"path\":\"main.go\"} ")
	if string(got) != "{\"path\":\"main.go\"}" {
		t.Fatalf("unexpected normalized json: %q", string(got))
	}

	first := RawJSONFromString("{\"a\":1}")
	first[0] = '['
	second := RawJSONFromString("{\"a\":1}")
	if string(second) != "{\"a\":1}" {
		t.Fatalf("expected detached payload, got %q", string(second))
	}
}

func TestMarshalToolInput(t *testing.T) {
	t.Parallel()

	gotNil, err := MarshalToolInput(nil)
	if err != nil {
		t.Fatalf("MarshalToolInput(nil) error = %v", err)
	}
	if string(gotNil) != "{}" {
		t.Fatalf("MarshalToolInput(nil) = %q, want {}", string(gotNil))
	}

	gotMap, err := MarshalToolInput(map[string]any{"path": "main.go"})
	if err != nil {
		t.Fatalf("MarshalToolInput(map) error = %v", err)
	}
	if !json.Valid(gotMap) {
		t.Fatalf("MarshalToolInput(map) produced invalid json: %q", string(gotMap))
	}
	var decoded map[string]any
	if err := json.Unmarshal(gotMap, &decoded); err != nil {
		t.Fatalf("unmarshal map output: %v", err)
	}
	if decoded["path"] != "main.go" {
		t.Fatalf("unexpected decoded map output: %#v", decoded)
	}

	if _, err := MarshalToolInput(invalidJSONMarshaler{}); err == nil {
		t.Fatalf("expected error for invalid json marshaler output")
	}
	if _, err := MarshalToolInput(errJSONMarshaler{}); err == nil {
		t.Fatalf("expected marshal error")
	}

	gotEmpty, err := MarshalToolInput(emptyJSONMarshaler{})
	if err != nil {
		t.Fatalf("MarshalToolInput(emptyJSONMarshaler) error = %v", err)
	}
	if string(gotEmpty) != "{}" {
		t.Fatalf("MarshalToolInput(emptyJSONMarshaler) = %q, want {}", string(gotEmpty))
	}
}
