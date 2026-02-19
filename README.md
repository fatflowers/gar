# gar

gar (Go Agent Runtime) is a minimal TUI-based coding agent written in Go.

## Status

Core runtime pieces are implemented and tested:
- Canonical `internal/llm` layer with Anthropic + mock providers
- Agent loop with tool-use execution, steering/follow-up queues, and cancellation
- `internal/agent/session` core loop abstraction (session tree/branch, context compaction, queue tracking)
- Shared built-in tools in `internal/agent/tool`: `read`, `write`, `edit`, `bash`, `find`, `grep`, `ls`
- Coding-agent tool composition in `internal/coding-agent/tool`
- Shared slash-command runtime in `internal/agentapp`
- Session JSONL persistence + TUI session recorder
- BubbleTea-based TUI with basic slash commands (`/help`, `/session`, `/name`, `/new`, `/resume`, `/tree`, `/branch`, `/fork`, `/compact`, `/queue`, `/dequeue`)
- Cobra CLI entrypoint
