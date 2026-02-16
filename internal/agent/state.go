package agent

// State is the high-level runtime status of the agent.
type State string

const (
	StateIdle          State = "idle"
	StateStreaming     State = "streaming"
	StateToolExecuting State = "tool_executing"
	StateError         State = "error"
)
