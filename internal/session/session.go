package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionDirName = ".gar/sessions"
	sessionFileExt        = ".jsonl"
	maxJSONLLineSize      = 1024 * 1024
)

var (
	ErrSessionDirRequired = errors.New("session directory is required")
	ErrSessionIDRequired  = errors.New("session id is required")
	ErrInvalidSessionID   = errors.New("invalid session id")
	ErrEntryIDRequired    = errors.New("entry id is required")
	ErrEntryTypeRequired  = errors.New("entry type is required")
	ErrSessionNotFound    = errors.New("session not found")
)

// Entry is one append-only record in a session JSONL file.
type Entry struct {
	ID         string          `json:"id"`
	ParentID   string          `json:"parent_id,omitempty"`
	Type       string          `json:"type"`
	Content    string          `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Params     json.RawMessage `json:"params,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	TS         int64           `json:"ts"`
}

// SessionInfo describes one session file on disk.
type SessionInfo struct {
	ID        string
	Path      string
	UpdatedAt time.Time
	SizeBytes int64
}

// Store persists session entries as append-only JSONL files.
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore constructs a session store rooted at dir.
func NewStore(dir string) (*Store, error) {
	root := strings.TrimSpace(dir)
	if root == "" {
		return nil, ErrSessionDirRequired
	}
	return &Store{dir: root}, nil
}

// DefaultDir returns the canonical sessions directory under a project root.
func DefaultDir(projectRoot string) string {
	return filepath.Join(projectRoot, defaultSessionDirName)
}

// Append appends one entry to a session file.
func (s *Store) Append(ctx context.Context, sessionID string, entry Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := s.sessionPath(sessionID)
	if err != nil {
		return err
	}

	entry.ID = strings.TrimSpace(entry.ID)
	entry.Type = strings.TrimSpace(entry.Type)
	if entry.ID == "" {
		return ErrEntryIDRequired
	}
	if entry.Type == "" {
		return ErrEntryTypeRequired
	}
	if entry.TS <= 0 {
		entry.TS = time.Now().Unix()
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal session entry: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create session dir %s: %w", s.dir, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open session file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Write(raw); err != nil {
		return fmt.Errorf("append session entry: %w", err)
	}
	if _, err := file.Write([]byte("\n")); err != nil {
		return fmt.Errorf("append session newline: %w", err)
	}
	return nil
}

// Load reads all entries from one session file.
func (s *Store) Load(ctx context.Context, sessionID string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, err := s.sessionPath(sessionID)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, strings.TrimSpace(sessionID))
		}
		return nil, fmt.Errorf("open session file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize)

	entries := make([]Entry, 0, 64)
	lineNum := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("decode session line %d: %w", lineNum, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("decode session line too large (> %d bytes): %w", maxJSONLLineSize, err)
		}
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		return nil, fmt.Errorf("scan session file: %w", err)
	}

	return entries, nil
}

// List returns known session files sorted by newest first.
func (s *Store) List(ctx context.Context) ([]SessionInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	items, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session dir %s: %w", s.dir, err)
	}

	out := make([]SessionInfo, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.IsDir() || filepath.Ext(item.Name()) != sessionFileExt {
			continue
		}

		info, err := item.Info()
		if err != nil {
			return nil, fmt.Errorf("read session file info %s: %w", item.Name(), err)
		}

		id := strings.TrimSuffix(item.Name(), sessionFileExt)
		out = append(out, SessionInfo{
			ID:        id,
			Path:      filepath.Join(s.dir, item.Name()),
			UpdatedAt: info.ModTime(),
			SizeBytes: info.Size(),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Store) sessionPath(sessionID string) (string, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "", ErrSessionIDRequired
	}
	if strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return "", fmt.Errorf("%w: %s", ErrInvalidSessionID, id)
	}
	return filepath.Join(s.dir, id+sessionFileExt), nil
}
