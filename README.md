# gar

gar (Go Agent Runtime) is a minimal TUI-based coding agent written in Go.

## Status

Core runtime pieces are implemented and tested:
- Canonical `internal/llm` layer with Anthropic + mock providers
- Agent loop with tool-use execution, steering/follow-up queues, and cancellation
- Built-in tools: `read`, `write`, `edit`, `bash`
- Session JSONL persistence + TUI session recorder
- BubbleTea-based TUI skeleton and Cobra CLI entrypoint
