package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gar/internal/llm"
	sessionstore "gar/internal/session"
)

const (
	defaultAutoCompactMessages = 80
	defaultCompactionKeep      = 24
	compactionSummaryMaxLines  = 40
	compactionSummaryMaxChars  = 6000
)

var (
	ErrRunnerRequired       = errors.New("agent session runner is required")
	ErrSessionIDRequired    = errors.New("agent session id is required")
	ErrSessionStoreRequired = errors.New("session store is required")
	ErrQueueUnsupported     = errors.New("runner does not support queued messages")
	ErrBranchTargetNotFound = errors.New("branch target not found")
	ErrCompactionNotNeeded  = errors.New("compaction not needed")
)

// Runner executes one LLM request as an event stream.
type Runner interface {
	Run(ctx context.Context, req *llm.Request) (<-chan llm.Event, error)
}

// QueueRunner is the optional queue control contract (steer/follow-up).
type QueueRunner interface {
	Steer(msg llm.Message)
	FollowUp(msg llm.Message)
	ClearAllQueues()
}

// Config configures one AgentSession.
type Config struct {
	Runner              Runner
	Store               *sessionstore.Store
	SessionID           string
	Model               string
	MaxTokens           int
	Tools               []llm.ToolSpec
	Meta                map[string]any
	AutoCompactMessages int
	CompactionKeep      int
}

// CompactionResult reports one compaction run.
type CompactionResult struct {
	Summary         string
	DroppedMessages int
	FirstKeptEntry  string
}

// Stats contains session counters for /session.
type Stats struct {
	SessionID       string
	SessionName     string
	LeafID          string
	EntryCount      int
	UserMessages    int
	AssistantMsgs   int
	ToolCalls       int
	ToolResults     int
	SteeringQueued  int
	FollowUpQueued  int
	ConversationLen int
}

// TreeNode is one node in the current session tree.
type TreeNode struct {
	Entry    sessionstore.Entry
	Children []TreeNode
}

// AgentSession is the core coding-agent loop abstraction for gar.
type AgentSession struct {
	runner      Runner
	queueRunner QueueRunner
	store       *sessionstore.Store

	sessionID string
	model     string
	maxTokens int
	tools     []llm.ToolSpec
	baseMeta  map[string]any

	autoCompactMessages int
	compactionKeep      int

	mu              sync.Mutex
	entries         []sessionstore.Entry
	byID            map[string]sessionstore.Entry
	leafID          string
	nextEntryID     int
	conversation    []llm.Message
	assistantBuffer strings.Builder
	latestUsage     *llm.Usage
	steeringQueued  []string
	followUpQueued  []string
	sessionName     string
}

// New constructs an AgentSession and loads any existing JSONL entries.
func New(ctx context.Context, cfg Config) (*AgentSession, error) {
	if cfg.Runner == nil {
		return nil, ErrRunnerRequired
	}
	id := strings.TrimSpace(cfg.SessionID)
	if id == "" {
		return nil, ErrSessionIDRequired
	}

	s := &AgentSession{
		runner:              cfg.Runner,
		store:               cfg.Store,
		sessionID:           id,
		model:               strings.TrimSpace(cfg.Model),
		maxTokens:           cfg.MaxTokens,
		tools:               cloneToolSpecs(cfg.Tools),
		baseMeta:            cloneMeta(cfg.Meta),
		autoCompactMessages: cfg.AutoCompactMessages,
		compactionKeep:      cfg.CompactionKeep,
		byID:                make(map[string]sessionstore.Entry),
	}
	if s.autoCompactMessages <= 0 {
		s.autoCompactMessages = defaultAutoCompactMessages
	}
	if s.compactionKeep <= 0 {
		s.compactionKeep = defaultCompactionKeep
	}

	if runner, ok := cfg.Runner.(QueueRunner); ok {
		s.queueRunner = runner
	}

	if cfg.Store != nil {
		loaded, err := cfg.Store.Load(ctx, id)
		if err != nil && !errors.Is(err, sessionstore.ErrSessionNotFound) {
			return nil, err
		}
		if len(loaded) > 0 {
			s.entries = append(s.entries, loaded...)
		}
	}

	s.reindexLocked()
	s.conversation = s.rebuildConversationLocked()

	if len(s.entries) == 0 && len(s.baseMeta) > 0 {
		if err := s.AppendMeta(ctx, s.baseMeta); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// SessionID returns the current logical session id.
func (s *AgentSession) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

// LeafID returns the current branch leaf entry id.
func (s *AgentSession) LeafID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leafID
}

// SessionName returns the current user-visible session name.
func (s *AgentSession) SessionName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionName
}

// Messages returns a defensive copy of current conversation context.
func (s *AgentSession) Messages() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMessages(s.conversation)
}

// Entries returns a defensive copy of all known session entries.
func (s *AgentSession) Entries() []sessionstore.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make([]sessionstore.Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		copied = append(copied, cloneEntry(entry))
	}
	return copied
}

// Stats returns queue and session counters.
func (s *AgentSession) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := Stats{
		SessionID:       s.sessionID,
		SessionName:     s.sessionName,
		LeafID:          s.leafID,
		EntryCount:      len(s.entries),
		SteeringQueued:  len(s.steeringQueued),
		FollowUpQueued:  len(s.followUpQueued),
		ConversationLen: len(s.conversation),
	}
	for _, entry := range s.entries {
		switch entry.Type {
		case "user":
			stats.UserMessages++
		case "assistant":
			stats.AssistantMsgs++
		case "tool_call":
			stats.ToolCalls++
		case "tool_result":
			stats.ToolResults++
		}
	}
	return stats
}

// AppendMeta appends one metadata entry.
func (s *AgentSession) AppendMeta(ctx context.Context, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEntryLocked(ctx, sessionstore.Entry{
		Type: "meta",
		Data: raw,
	})
}

// SetSessionName stores one display name entry and updates in-memory state.
func (s *AgentSession) SetSessionName(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmed := strings.TrimSpace(name)
	if err := s.appendEntryLocked(ctx, sessionstore.Entry{
		Type: "session_info",
		Name: trimmed,
	}); err != nil {
		return err
	}
	s.sessionName = trimmed
	return nil
}

// ListSessions returns persisted sessions sorted by newest first.
func (s *AgentSession) ListSessions(ctx context.Context) ([]sessionstore.SessionInfo, error) {
	if s.store == nil {
		return nil, ErrSessionStoreRequired
	}
	return s.store.List(ctx)
}

// SwitchSession loads another session file into the current runtime.
func (s *AgentSession) SwitchSession(ctx context.Context, sessionID string) error {
	if s.store == nil {
		return ErrSessionStoreRequired
	}
	target := strings.TrimSpace(sessionID)
	if target == "" {
		return ErrSessionIDRequired
	}

	loaded, err := s.store.Load(ctx, target)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.switchSessionLocked(target, loaded)
	return nil
}

// NewSession resets state to a fresh logical session id.
func (s *AgentSession) NewSession(ctx context.Context, requestedID string) (string, error) {
	id := strings.TrimSpace(requestedID)
	if id == "" {
		id = s.generateSessionID(ctx)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.switchSessionLocked(id, nil)
	if len(s.baseMeta) > 0 {
		rawMeta, err := json.Marshal(s.baseMeta)
		if err != nil {
			return "", fmt.Errorf("marshal meta: %w", err)
		}
		if err := s.appendEntryLocked(ctx, sessionstore.Entry{
			Type: "meta",
			Data: rawMeta,
		}); err != nil {
			return "", err
		}
	}
	return s.sessionID, nil
}

// Submit appends a user message and starts one run.
func (s *AgentSession) Submit(ctx context.Context, text string) (<-chan llm.Event, error) {
	content := strings.TrimSpace(text)
	if content == "" {
		return nil, nil
	}

	s.mu.Lock()
	if err := s.appendUserLocked(ctx, content); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if _, err := s.compactLocked(ctx, s.autoCompactMessages, s.compactionKeep, ""); err != nil && !errors.Is(err, ErrCompactionNotNeeded) {
		s.mu.Unlock()
		return nil, err
	}
	req := s.buildRequestLocked()
	s.mu.Unlock()

	return s.runner.Run(ctx, req)
}

// Run starts one run without appending a new user message.
func (s *AgentSession) Run(ctx context.Context) (<-chan llm.Event, error) {
	s.mu.Lock()
	if _, err := s.compactLocked(ctx, s.autoCompactMessages, s.compactionKeep, ""); err != nil && !errors.Is(err, ErrCompactionNotNeeded) {
		s.mu.Unlock()
		return nil, err
	}
	req := s.buildRequestLocked()
	s.mu.Unlock()
	return s.runner.Run(ctx, req)
}

// QueueSteer queues a high-priority user message when the runner supports queues.
func (s *AgentSession) QueueSteer(text string) error {
	content := strings.TrimSpace(text)
	if content == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queueRunner == nil {
		return ErrQueueUnsupported
	}
	s.steeringQueued = append(s.steeringQueued, content)
	s.queueRunner.Steer(userTextMessage(content))
	return nil
}

// QueueFollowUp queues a low-priority user message when the runner supports queues.
func (s *AgentSession) QueueFollowUp(text string) error {
	content := strings.TrimSpace(text)
	if content == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queueRunner == nil {
		return ErrQueueUnsupported
	}
	s.followUpQueued = append(s.followUpQueued, content)
	s.queueRunner.FollowUp(userTextMessage(content))
	return nil
}

// SteeringQueued returns queued steering messages.
func (s *AgentSession) SteeringQueued() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steeringQueued...)
}

// FollowUpQueued returns queued follow-up messages.
func (s *AgentSession) FollowUpQueued() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.followUpQueued...)
}

// ClearQueue clears queued messages and returns them.
func (s *AgentSession) ClearQueue() (steering []string, followUp []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	steering = append([]string(nil), s.steeringQueued...)
	followUp = append([]string(nil), s.followUpQueued...)
	s.steeringQueued = nil
	s.followUpQueued = nil
	if s.queueRunner != nil {
		s.queueRunner.ClearAllQueues()
	}
	return steering, followUp
}

// RecordEvent consumes one stream event and updates session state.
func (s *AgentSession) RecordEvent(ctx context.Context, ev llm.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch ev.Type {
	case llm.EventQueuedMessage:
		if ev.Message == nil || ev.Message.Role != llm.RoleUser {
			return nil
		}
		text := strings.TrimSpace(messageText(*ev.Message))
		if text == "" {
			return nil
		}
		s.dequeueDeliveredLocked(text)
		return s.appendUserLocked(ctx, text)
	case llm.EventContentBlockStart:
		if ev.ContentBlockStart != nil && ev.ContentBlockStart.Type == "text" && ev.ContentBlockStart.Text != "" {
			s.assistantBuffer.WriteString(ev.ContentBlockStart.Text)
		}
		return nil
	case llm.EventTextDelta:
		s.assistantBuffer.WriteString(ev.TextDelta)
		return nil
	case llm.EventToolCallStart:
		if ev.ToolCall == nil {
			return nil
		}
		return s.appendEntryLocked(ctx, sessionstore.Entry{
			Type:   "tool_call",
			Name:   ev.ToolCall.Name,
			Params: append(json.RawMessage(nil), ev.ToolCall.Arguments...),
		})
	case llm.EventToolResult:
		if ev.ToolResult == nil {
			return nil
		}
		state, err := json.Marshal(map[string]any{"is_error": ev.ToolResult.IsError})
		if err != nil {
			return fmt.Errorf("marshal tool_result state: %w", err)
		}
		s.conversation = append(s.conversation, llm.Message{
			Role: llm.RoleTool,
			ToolResult: &llm.ToolResult{
				ToolCallID: ev.ToolResult.ToolCallID,
				ToolName:   ev.ToolResult.ToolName,
				Content:    ev.ToolResult.Content,
				IsError:    ev.ToolResult.IsError,
			},
		})
		return s.appendEntryLocked(ctx, sessionstore.Entry{
			Type:       "tool_result",
			ToolCallID: ev.ToolResult.ToolCallID,
			Name:       ev.ToolResult.ToolName,
			Content:    ev.ToolResult.Content,
			Data:       state,
		})
	case llm.EventUsage:
		if ev.Usage != nil {
			usage := *ev.Usage
			s.latestUsage = &usage
		}
		return nil
	case llm.EventDone, llm.EventError:
		return s.flushAssistantLocked(ctx)
	default:
		return nil
	}
}

// Finalize flushes any buffered assistant text.
func (s *AgentSession) Finalize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushAssistantLocked(ctx)
}

// Compact runs manual compaction keeping the newest keepMessages conversation messages.
func (s *AgentSession) Compact(ctx context.Context, keepMessages int, instructions string) (CompactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if keepMessages <= 0 {
		keepMessages = s.compactionKeep
	}
	return s.compactLocked(ctx, 0, keepMessages, instructions)
}

// SwitchBranch moves the leaf pointer to targetID and rebuilds conversation context.
func (s *AgentSession) SwitchBranch(targetID string) error {
	target := strings.TrimSpace(targetID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if target == "" {
		s.leafID = ""
		s.conversation = nil
		s.assistantBuffer.Reset()
		s.latestUsage = nil
		return nil
	}
	if _, ok := s.byID[target]; !ok {
		return fmt.Errorf("%w: %s", ErrBranchTargetNotFound, target)
	}
	s.leafID = target
	s.conversation = s.rebuildConversationLocked()
	s.assistantBuffer.Reset()
	s.latestUsage = nil
	return nil
}

// Tree returns the current session entry tree.
func (s *AgentSession) Tree() []TreeNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return buildTree(s.entries, s.byID)
}

// TreeLines renders the tree for slash command output.
func (s *AgentSession) TreeLines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	roots := buildTree(s.entries, s.byID)
	if len(roots) == 0 {
		return nil
	}
	lines := make([]string, 0, len(s.entries))
	var walk func(node TreeNode, depth int)
	walk = func(node TreeNode, depth int) {
		indent := strings.Repeat("  ", depth)
		marker := " "
		if node.Entry.ID == s.leafID {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("%s %s%s %s", marker, indent, node.Entry.ID, entryPreview(node.Entry)))
		for _, child := range node.Children {
			walk(child, depth+1)
		}
	}
	for _, root := range roots {
		walk(root, 0)
	}
	return lines
}

func (s *AgentSession) buildRequestLocked() *llm.Request {
	return &llm.Request{
		Model:     s.model,
		Messages:  cloneMessages(s.conversation),
		Tools:     cloneToolSpecs(s.tools),
		MaxTokens: s.maxTokens,
	}
}

func (s *AgentSession) appendUserLocked(ctx context.Context, content string) error {
	s.conversation = append(s.conversation, userTextMessage(content))
	return s.appendEntryLocked(ctx, sessionstore.Entry{
		Type:    "user",
		Content: content,
	})
}

func (s *AgentSession) flushAssistantLocked(ctx context.Context) error {
	text := strings.TrimSpace(s.assistantBuffer.String())
	if text == "" {
		return nil
	}
	entry := sessionstore.Entry{
		Type:    "assistant",
		Content: text,
	}
	if s.latestUsage != nil {
		raw, err := json.Marshal(s.latestUsage)
		if err != nil {
			return fmt.Errorf("marshal usage: %w", err)
		}
		entry.Usage = raw
	}

	s.conversation = append(s.conversation, llm.Message{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{{
			Type: llm.ContentTypeText,
			Text: text,
		}},
	})

	if err := s.appendEntryLocked(ctx, entry); err != nil {
		return err
	}

	s.assistantBuffer.Reset()
	s.latestUsage = nil
	return nil
}

func (s *AgentSession) compactLocked(
	ctx context.Context,
	threshold int,
	keepMessages int,
	instructions string,
) (CompactionResult, error) {
	if threshold > 0 {
		conversationMessages := countConversationMessages(s.conversation)
		if conversationMessages <= threshold {
			return CompactionResult{}, ErrCompactionNotNeeded
		}
	}

	if keepMessages <= 0 {
		keepMessages = s.compactionKeep
	}

	branch := s.branchEntriesLocked(s.leafID)
	messageEntries := make([]sessionstore.Entry, 0, len(branch))
	for _, entry := range branch {
		if isMessageEntry(entry) {
			messageEntries = append(messageEntries, entry)
		}
	}
	if len(messageEntries) <= keepMessages {
		return CompactionResult{}, ErrCompactionNotNeeded
	}

	firstKept := messageEntries[len(messageEntries)-keepMessages]
	dropped := messageEntries[:len(messageEntries)-keepMessages]
	firstKeptID := firstKept.ID
	summary := buildCompactionSummary(dropped, instructions)

	details := map[string]any{
		"first_kept_entry_id": firstKeptID,
		"dropped_messages":    len(dropped),
	}
	if strings.TrimSpace(instructions) != "" {
		details["instructions"] = strings.TrimSpace(instructions)
	}
	rawDetails, err := json.Marshal(details)
	if err != nil {
		return CompactionResult{}, fmt.Errorf("marshal compaction details: %w", err)
	}

	if err := s.appendEntryLocked(ctx, sessionstore.Entry{
		Type:    "compaction",
		Content: summary,
		Data:    rawDetails,
	}); err != nil {
		return CompactionResult{}, err
	}

	s.conversation = s.rebuildConversationLocked()
	return CompactionResult{
		Summary:         summary,
		DroppedMessages: len(dropped),
		FirstKeptEntry:  firstKeptID,
	}, nil
}

func (s *AgentSession) appendEntryLocked(ctx context.Context, entry sessionstore.Entry) error {
	entry.ID = fmt.Sprintf("%06d", s.nextEntryID)
	entry.ParentID = s.leafID
	if entry.TS <= 0 {
		entry.TS = time.Now().Unix()
	}

	if s.store != nil {
		if err := s.store.Append(ctx, s.sessionID, entry); err != nil {
			return err
		}
	}

	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = entry
	s.leafID = entry.ID
	s.nextEntryID++
	return nil
}

func (s *AgentSession) branchEntriesLocked(leafID string) []sessionstore.Entry {
	leaf := strings.TrimSpace(leafID)
	if leaf == "" {
		return nil
	}

	path := make([]sessionstore.Entry, 0, len(s.entries))
	current := leaf
	visited := make(map[string]struct{}, len(s.entries))

	for current != "" {
		if _, seen := visited[current]; seen {
			break
		}
		visited[current] = struct{}{}
		entry, ok := s.byID[current]
		if !ok {
			break
		}
		path = append(path, entry)
		current = strings.TrimSpace(entry.ParentID)
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func (s *AgentSession) rebuildConversationLocked() []llm.Message {
	branch := s.branchEntriesLocked(s.leafID)
	if len(branch) == 0 {
		return nil
	}

	latestCompactionIndex := -1
	firstKeptID := ""
	compactionSummary := ""

	for i, entry := range branch {
		if entry.Type != "compaction" {
			continue
		}
		latestCompactionIndex = i
		firstKeptID = compactionFirstKeptID(entry)
		compactionSummary = strings.TrimSpace(entry.Content)
	}

	messages := make([]llm.Message, 0, len(branch))
	appendEntryMessage := func(entry sessionstore.Entry) {
		msg, ok := entryToMessage(entry)
		if !ok {
			return
		}
		messages = append(messages, msg)
	}

	if latestCompactionIndex < 0 {
		for _, entry := range branch {
			appendEntryMessage(entry)
		}
		return messages
	}

	if compactionSummary != "" {
		messages = append(messages, llm.Message{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{{
				Type: llm.ContentTypeText,
				Text: compactionSummary,
			}},
		})
	}

	start := latestCompactionIndex
	if firstKeptID != "" {
		for i := 0; i < latestCompactionIndex; i++ {
			if branch[i].ID == firstKeptID {
				start = i
				break
			}
		}
	}
	for i := start; i < latestCompactionIndex; i++ {
		appendEntryMessage(branch[i])
	}
	for i := latestCompactionIndex + 1; i < len(branch); i++ {
		appendEntryMessage(branch[i])
	}

	return messages
}

func (s *AgentSession) dequeueDeliveredLocked(text string) {
	for i, queued := range s.steeringQueued {
		if queued != text {
			continue
		}
		s.steeringQueued = append(append([]string(nil), s.steeringQueued[:i]...), s.steeringQueued[i+1:]...)
		return
	}
	for i, queued := range s.followUpQueued {
		if queued != text {
			continue
		}
		s.followUpQueued = append(append([]string(nil), s.followUpQueued[:i]...), s.followUpQueued[i+1:]...)
		return
	}
}

func (s *AgentSession) reindexLocked() {
	s.byID = make(map[string]sessionstore.Entry, len(s.entries))
	s.leafID = ""
	s.sessionName = ""
	maxNumericID := 0
	for _, entry := range s.entries {
		s.byID[entry.ID] = entry
		s.leafID = entry.ID
		if entry.Type == "session_info" {
			s.sessionName = strings.TrimSpace(entry.Name)
		}
		if parsed, err := strconv.Atoi(entry.ID); err == nil && parsed > maxNumericID {
			maxNumericID = parsed
		}
	}
	if maxNumericID == 0 {
		maxNumericID = len(s.entries)
	}
	s.nextEntryID = maxNumericID + 1
}

func (s *AgentSession) switchSessionLocked(sessionID string, entries []sessionstore.Entry) {
	s.sessionID = strings.TrimSpace(sessionID)
	s.entries = append([]sessionstore.Entry(nil), entries...)
	s.reindexLocked()
	s.conversation = s.rebuildConversationLocked()
	s.assistantBuffer.Reset()
	s.latestUsage = nil
	s.steeringQueued = nil
	s.followUpQueued = nil
	if s.queueRunner != nil {
		s.queueRunner.ClearAllQueues()
	}
}

func (s *AgentSession) generateSessionID(ctx context.Context) string {
	base := time.Now().UTC().Format("20060102-150405")
	if s.store == nil {
		return base
	}

	infos, err := s.store.List(ctx)
	if err != nil || len(infos) == 0 {
		return base
	}

	used := make(map[string]struct{}, len(infos))
	for _, info := range infos {
		used[strings.TrimSpace(info.ID)] = struct{}{}
	}
	if _, exists := used[base]; !exists {
		return base
	}

	for i := 1; i < 10_000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UTC().UnixNano())
}

func entryToMessage(entry sessionstore.Entry) (llm.Message, bool) {
	switch entry.Type {
	case "user":
		text := strings.TrimSpace(entry.Content)
		if text == "" {
			return llm.Message{}, false
		}
		return userTextMessage(text), true
	case "assistant":
		text := strings.TrimSpace(entry.Content)
		if text == "" {
			return llm.Message{}, false
		}
		return llm.Message{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{{
				Type: llm.ContentTypeText,
				Text: text,
			}},
		}, true
	case "tool_result":
		isError := false
		if len(entry.Data) > 0 {
			var state struct {
				IsError bool `json:"is_error"`
			}
			if err := json.Unmarshal(entry.Data, &state); err == nil {
				isError = state.IsError
			}
		}
		return llm.Message{
			Role: llm.RoleTool,
			ToolResult: &llm.ToolResult{
				ToolCallID: entry.ToolCallID,
				ToolName:   entry.Name,
				Content:    entry.Content,
				IsError:    isError,
			},
		}, true
	default:
		return llm.Message{}, false
	}
}

func isMessageEntry(entry sessionstore.Entry) bool {
	switch entry.Type {
	case "user", "assistant", "tool_result":
		return true
	default:
		return false
	}
}

func buildCompactionSummary(entries []sessionstore.Entry, instructions string) string {
	lines := make([]string, 0, len(entries)+3)
	lines = append(lines, "[Context Compact Summary]")
	if trimmed := strings.TrimSpace(instructions); trimmed != "" {
		lines = append(lines, "Instructions: "+trimmed)
	}
	lines = append(lines, "Earlier conversation highlights:")

	count := 0
	for _, entry := range entries {
		role := entry.Type
		text := strings.TrimSpace(entry.Content)
		if entry.Type == "tool_result" {
			if strings.TrimSpace(entry.Name) != "" {
				role = "tool:" + strings.TrimSpace(entry.Name)
			}
			if text == "" {
				text = "(empty tool result)"
			}
		}
		if text == "" {
			continue
		}
		text = truncateRunes(text, 180)
		lines = append(lines, fmt.Sprintf("- %s: %s", role, text))
		count++
		if count >= compactionSummaryMaxLines {
			break
		}
	}
	if count == 0 {
		lines = append(lines, "- (no textual messages)")
	}

	summary := strings.Join(lines, "\n")
	if len(summary) > compactionSummaryMaxChars {
		summary = summary[:compactionSummaryMaxChars]
	}
	return summary
}

func compactionFirstKeptID(entry sessionstore.Entry) string {
	if len(entry.Data) == 0 {
		return ""
	}
	var payload struct {
		FirstKeptEntryID string `json:"first_kept_entry_id"`
	}
	if err := json.Unmarshal(entry.Data, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.FirstKeptEntryID)
}

func messageText(message llm.Message) string {
	if len(message.Content) == 0 {
		if message.ToolResult != nil {
			return strings.TrimSpace(message.ToolResult.Content)
		}
		return ""
	}
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if block.Type != llm.ContentTypeText {
			continue
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func userTextMessage(text string) llm.Message {
	return llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{{
			Type: llm.ContentTypeText,
			Text: text,
		}},
	}
}

func buildTree(entries []sessionstore.Entry, byID map[string]sessionstore.Entry) []TreeNode {
	if len(entries) == 0 {
		return nil
	}
	children := make(map[string][]string, len(entries))
	roots := make([]string, 0, len(entries))
	for _, entry := range entries {
		parent := strings.TrimSpace(entry.ParentID)
		if parent == "" {
			roots = append(roots, entry.ID)
			continue
		}
		if _, ok := byID[parent]; !ok {
			roots = append(roots, entry.ID)
			continue
		}
		children[parent] = append(children[parent], entry.ID)
	}

	var visit func(string) TreeNode
	visit = func(id string) TreeNode {
		entry := byID[id]
		node := TreeNode{Entry: cloneEntry(entry)}
		for _, childID := range children[id] {
			node.Children = append(node.Children, visit(childID))
		}
		return node
	}

	out := make([]TreeNode, 0, len(roots))
	for _, rootID := range roots {
		out = append(out, visit(rootID))
	}
	return out
}

func entryPreview(entry sessionstore.Entry) string {
	typeName := strings.TrimSpace(entry.Type)
	if typeName == "" {
		typeName = "entry"
	}

	snippet := ""
	switch entry.Type {
	case "user", "assistant", "compaction":
		snippet = strings.TrimSpace(entry.Content)
	case "session_info":
		snippet = strings.TrimSpace(entry.Name)
	case "tool_call", "tool_result":
		snippet = strings.TrimSpace(entry.Name)
		if snippet == "" {
			snippet = strings.TrimSpace(entry.Content)
		}
	}
	if snippet == "" {
		return typeName
	}
	return fmt.Sprintf("%s %s", typeName, truncateRunes(snippet, 48))
}

func countConversationMessages(messages []llm.Message) int {
	count := 0
	for _, message := range messages {
		if message.Role == llm.RoleUser || message.Role == llm.RoleAssistant || message.Role == llm.RoleTool {
			count++
		}
	}
	return count
}

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "..."
}

func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		copyMsg := llm.Message{
			Role:      message.Role,
			Content:   append([]llm.ContentBlock(nil), message.Content...),
			ToolCalls: append([]llm.ToolCall(nil), message.ToolCalls...),
		}
		if message.ToolResult != nil {
			result := *message.ToolResult
			copyMsg.ToolResult = &result
		}
		out = append(out, copyMsg)
	}
	return out
}

func cloneToolSpecs(specs []llm.ToolSpec) []llm.ToolSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		copySpec := spec
		copySpec.Schema = append(json.RawMessage(nil), spec.Schema...)
		out = append(out, copySpec)
	}
	return out
}

func cloneEntry(entry sessionstore.Entry) sessionstore.Entry {
	cloned := entry
	cloned.Params = append(json.RawMessage(nil), entry.Params...)
	cloned.Data = append(json.RawMessage(nil), entry.Data...)
	cloned.Usage = append(json.RawMessage(nil), entry.Usage...)
	return cloned
}

func cloneMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]any, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}

// SortEntriesByTimestampDesc sorts entries newest first. Useful for future UI commands.
func SortEntriesByTimestampDesc(entries []sessionstore.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TS == entries[j].TS {
			return entries[i].ID > entries[j].ID
		}
		return entries[i].TS > entries[j].TS
	})
}
