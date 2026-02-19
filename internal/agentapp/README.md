# internal/agentapp

Shared runtime orchestration layer for interactive agents.

## Responsibility

- Parse and execute shared slash commands.
- Coordinate command-time interactions with session/runtime capabilities.
- Stay UI-framework agnostic through adapter hooks.

## Import Boundaries

- May import shared runtime packages:
  - `internal/agent/session`
  - `internal/session`
- Must not import agent-specific packages such as:
  - `internal/coding-agent/*`
  - `internal/deepresearch-agent/*`
- `internal/tui` should call into `agentapp` for command execution rather than embedding agent-specific command switches.

## Notes

- Commands are centralized here (`/help`, `/session`, `/name`, `/new`, `/resume`, `/tree`, `/branch`, `/fork`, `/compact`, `/queue`, `/dequeue`).
- Agent-specific behavior should be provided via capability adapters, not direct package coupling.

