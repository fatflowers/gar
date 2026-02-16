package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"gar/internal/llm"
	"gar/internal/tools"
)

const defaultMaxTurns = 50

const (
	maxToolResultContentLen = 10_000
	toolResultHeadLen       = 4_000
	toolResultTailLen       = 4_000
	toolResultTruncateMark  = "\n...[truncated]...\n"
)

// QueueMode controls how queued messages are dequeued between turns.
type QueueMode string

const (
	// QueueModeOneAtATime dequeues one queued message per turn.
	QueueModeOneAtATime QueueMode = "one-at-a-time"
	// QueueModeAll dequeues all queued messages in one turn.
	QueueModeAll QueueMode = "all"
)

var (
	// ErrProviderRequired indicates missing LLM provider dependency.
	ErrProviderRequired = errors.New("provider is required")
	// ErrAgentBusy indicates an attempt to start a new run while one is active.
	ErrAgentBusy = errors.New("agent is already running")
	// ErrRequestRequired indicates missing run request.
	ErrRequestRequired = errors.New("request is required")
	// ErrMaxTurnsExceeded indicates the loop reached the configured turn limit.
	ErrMaxTurnsExceeded = errors.New("max turns exceeded")
	// ErrInvalidQueueMode indicates an unknown queue mode.
	ErrInvalidQueueMode = errors.New("invalid queue mode")
	// ErrNoMessagesToContinue indicates Continue requires an existing conversation tail.
	ErrNoMessagesToContinue = errors.New("no messages to continue from")
	// ErrContinueFromAssistantTail indicates assistant-tail continue requires queued user input.
	ErrContinueFromAssistantTail = errors.New("cannot continue from assistant tail without queued messages")
)

// Config configures Agent creation.
type Config struct {
	Provider     llm.Provider
	ToolRegistry *tools.Registry
	MaxTurns     int
	SteeringMode QueueMode
	FollowUpMode QueueMode
}

// Agent orchestrates the model/tool loop and exposes stream events.
type Agent struct {
	provider     llm.Provider
	toolRegistry *tools.Registry
	maxTurns     int
	steeringMode QueueMode
	followUpMode QueueMode

	mu            sync.Mutex
	state         State
	cancel        context.CancelFunc
	steeringQueue []llm.Message
	followUpQueue []llm.Message
}

// New creates an agent with explicit dependencies.
func New(cfg Config) (*Agent, error) {
	if cfg.Provider == nil {
		return nil, ErrProviderRequired
	}

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	steeringMode, err := normalizeQueueMode(cfg.SteeringMode)
	if err != nil {
		return nil, fmt.Errorf("configure steering mode: %w", err)
	}
	followUpMode, err := normalizeQueueMode(cfg.FollowUpMode)
	if err != nil {
		return nil, fmt.Errorf("configure follow-up mode: %w", err)
	}

	return &Agent{
		provider:     cfg.Provider,
		toolRegistry: cfg.ToolRegistry,
		maxTurns:     maxTurns,
		steeringMode: steeringMode,
		followUpMode: followUpMode,
		state:        StateIdle,
	}, nil
}

// Run starts one agent turn sequence and returns a stream of provider events.
func (a *Agent) Run(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	if req == nil {
		return nil, ErrRequestRequired
	}

	a.mu.Lock()
	if a.state != StateIdle {
		a.mu.Unlock()
		return nil, ErrAgentBusy
	}

	request := cloneRequest(req)
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.state = StateStreaming
	a.mu.Unlock()

	out := make(chan llm.Event, 1)
	forwardedOut := make(chan llm.Event)
	forwardDone := make(chan struct{})

	go func() {
		defer close(forwardDone)
		forwardEvents(forwardedOut, out)
	}()

	go func() {
		hooks := runLoopHooks{
			dequeueSteeringMessages: a.dequeueSteeringMessages,
			dequeueFollowUpMessages: a.dequeueFollowUpMessages,
		}
		if a.toolRegistry != nil {
			hooks.executeToolCall = a.executeToolCall
		}

		terminalForwarded, err := runLoop(runCtx, a.provider, request, a.maxTurns, forwardedOut, hooks)
		if err != nil && !terminalForwarded {
			reason := llm.StopReasonError
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				reason = llm.StopReasonAborted
			}
			if reason == llm.StopReasonError {
				a.setState(StateError)
			}
			forwardedOut <- llm.Event{
				Type: llm.EventError,
				Done: &llm.DonePayload{Reason: reason},
				Err:  err,
			}
		}
		close(forwardedOut)
		<-forwardDone
		close(out)
		cancel()
		a.finishRun()
	}()

	return out, nil
}

// Continue resumes a conversation using existing context and queued messages.
func (a *Agent) Continue(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	if req == nil {
		return nil, ErrRequestRequired
	}
	if len(req.Messages) == 0 {
		return nil, ErrNoMessagesToContinue
	}

	last := req.Messages[len(req.Messages)-1]
	if last.Role == llm.RoleAssistant && !a.HasQueuedMessages() {
		return nil, ErrContinueFromAssistantTail
	}

	return a.Run(ctx, req)
}

// Cancel requests cancellation of the current run, if any.
func (a *Agent) Cancel() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Steer queues a high-priority message for the next turn.
func (a *Agent) Steer(msg llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steeringQueue = append(a.steeringQueue, cloneMessage(msg))
}

// FollowUp queues a low-priority message processed when steering is empty.
func (a *Agent) FollowUp(msg llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.followUpQueue = append(a.followUpQueue, cloneMessage(msg))
}

// HasQueuedMessages reports whether any steering/follow-up messages are queued.
func (a *Agent) HasQueuedMessages() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.steeringQueue) > 0 || len(a.followUpQueue) > 0
}

// ClearSteeringQueue drops queued steering messages.
func (a *Agent) ClearSteeringQueue() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steeringQueue = nil
}

// ClearFollowUpQueue drops queued follow-up messages.
func (a *Agent) ClearFollowUpQueue() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.followUpQueue = nil
}

// ClearAllQueues drops both steering and follow-up queues.
func (a *Agent) ClearAllQueues() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steeringQueue = nil
	a.followUpQueue = nil
}

// State returns the current agent state.
func (a *Agent) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *Agent) finishRun() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancel = nil
	a.state = StateIdle
}

func (a *Agent) dequeueSteeringMessages() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()

	return dequeueQueuedMessages(&a.steeringQueue, a.steeringMode)
}

func (a *Agent) dequeueFollowUpMessages() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()

	return dequeueQueuedMessages(&a.followUpQueue, a.followUpMode)
}

func (a *Agent) executeToolCall(ctx context.Context, call llm.ToolCall) (llm.Message, error) {
	a.setState(StateToolExecuting)
	defer a.setState(StateStreaming)

	result, err := a.toolRegistry.Execute(ctx, call.Name, call.Arguments)
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return llm.Message{}, err
	}

	content := result.Content
	if err != nil {
		if content == "" {
			content = fmt.Sprintf("error: %v", err)
		} else {
			content = fmt.Sprintf("%s\n\nerror: %v", content, err)
		}
	}
	if content == "" {
		content = "ok"
	}

	return llm.Message{
		Role: llm.RoleTool,
		ToolResult: &llm.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    truncateToolResultContent(content),
			IsError:    err != nil,
		},
	}, nil
}

func (a *Agent) setState(next State) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = next
}

func truncateToolResultContent(content string) string {
	if len(content) <= maxToolResultContentLen {
		return content
	}
	return content[:toolResultHeadLen] + toolResultTruncateMark + content[len(content)-toolResultTailLen:]
}

func dequeueQueuedMessages(queue *[]llm.Message, mode QueueMode) []llm.Message {
	if len(*queue) == 0 {
		return nil
	}

	switch mode {
	case QueueModeAll:
		msgs := cloneMessages(*queue)
		*queue = nil
		return msgs
	default:
		msg := cloneMessage((*queue)[0])
		*queue = append([]llm.Message(nil), (*queue)[1:]...)
		return []llm.Message{msg}
	}
}

func normalizeQueueMode(mode QueueMode) (QueueMode, error) {
	switch mode {
	case "", QueueModeOneAtATime:
		return QueueModeOneAtATime, nil
	case QueueModeAll:
		return QueueModeAll, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidQueueMode, mode)
	}
}

func cloneRequest(req *llm.Request) *llm.Request {
	if req == nil {
		return nil
	}

	cloned := *req
	cloned.Messages = cloneMessages(req.Messages)
	cloned.Tools = cloneTools(req.Tools)
	cloned.Metadata = cloneMetadata(req.Metadata)
	if req.Temperature != nil {
		value := *req.Temperature
		cloned.Temperature = &value
	}
	return &cloned
}

func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		cloned = append(cloned, cloneMessage(msg))
	}
	return cloned
}

func cloneMessage(msg llm.Message) llm.Message {
	copyMsg := llm.Message{
		Role:      msg.Role,
		Content:   append([]llm.ContentBlock(nil), msg.Content...),
		ToolCalls: cloneToolCalls(msg.ToolCalls),
	}
	if msg.ToolResult != nil {
		result := *msg.ToolResult
		copyMsg.ToolResult = &result
	}
	return copyMsg
}

func cloneToolCalls(calls []llm.ToolCall) []llm.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	cloned := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		toolCall := call
		toolCall.Arguments = append(json.RawMessage(nil), call.Arguments...)
		cloned = append(cloned, toolCall)
	}
	return cloned
}

func cloneTools(tools []llm.ToolSpec) []llm.ToolSpec {
	if len(tools) == 0 {
		return nil
	}

	cloned := make([]llm.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		copyTool := tool
		copyTool.Schema = append(json.RawMessage(nil), tool.Schema...)
		cloned = append(cloned, copyTool)
	}
	return cloned
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func sendTerminalEvent(out chan<- llm.Event, event llm.Event) {
	select {
	case out <- event:
	default:
	}
}
