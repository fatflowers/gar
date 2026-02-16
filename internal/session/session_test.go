package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAppendAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	store, err := NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.Append(context.Background(), "abc123", Entry{
		ID:       "01",
		Type:     "meta",
		Data:     mustRawJSON(t, `{"model":"claude-sonnet-4","cwd":"/repo"}`),
		TS:       1700000001,
		ParentID: "",
	})
	if err != nil {
		t.Fatalf("Append(meta) error = %v", err)
	}

	err = store.Append(context.Background(), "abc123", Entry{
		ID:       "02",
		Type:     "user",
		Content:  "read main.go",
		TS:       1700000002,
		ParentID: "01",
	})
	if err != nil {
		t.Fatalf("Append(user) error = %v", err)
	}

	entries, err := store.Load(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Load() entries = %d, want 2", len(entries))
	}
	if entries[0].ID != "01" || entries[0].Type != "meta" {
		t.Fatalf("first entry = %#v, want meta id=01", entries[0])
	}
	if entries[1].ID != "02" || entries[1].ParentID != "01" || entries[1].Content != "read main.go" {
		t.Fatalf("second entry = %#v, want user with parent and content", entries[1])
	}
}

func TestStoreLoadNotFound(t *testing.T) {
	t.Parallel()

	store, err := NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.Load(context.Background(), "missing")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Load() error = %v, want ErrSessionNotFound", err)
	}
}

func TestStoreListReturnsSessionFiles(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), ".gar", "sessions")
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Append(context.Background(), "s1", Entry{ID: "1", Type: "meta"}); err != nil {
		t.Fatalf("Append(s1) error = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := store.Append(context.Background(), "s2", Entry{ID: "1", Type: "meta"}); err != nil {
		t.Fatalf("Append(s2) error = %v", err)
	}

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List() count = %d, want 2", len(got))
	}
	if got[0].ID != "s2" || got[1].ID != "s1" {
		t.Fatalf("List() order ids = [%s %s], want [s2 s1]", got[0].ID, got[1].ID)
	}

	if _, err := os.Stat(got[0].Path); err != nil {
		t.Fatalf("session file path not found: %v", err)
	}
}

func TestStoreAppendFillsTimestampWhenMissing(t *testing.T) {
	t.Parallel()

	store, err := NewStore(filepath.Join(t.TempDir(), ".gar", "sessions"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Append(context.Background(), "ts", Entry{
		ID:   "01",
		Type: "assistant",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	entries, err := store.Load(context.Background(), "ts")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Load() entries = %d, want 1", len(entries))
	}
	if entries[0].TS <= 0 {
		t.Fatalf("TS = %d, want > 0", entries[0].TS)
	}
}

func mustRawJSON(t *testing.T, raw string) json.RawMessage {
	t.Helper()
	value := json.RawMessage(raw)
	if !json.Valid(value) {
		t.Fatalf("invalid json fixture: %s", raw)
	}
	return value
}
