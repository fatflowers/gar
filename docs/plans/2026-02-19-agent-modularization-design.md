# Agent Modularization Design

Date: 2026-02-19
Status: Approved
Scope: Extract agent-specific runtime code from shared layers so multiple agent products (coding/deepresearch) can coexist cleanly.

## 1. Goals

- Make `/Users/simon/gar/internal/agent` and `/Users/simon/gar/internal/tui` reusable shared modules.
- Move session runtime from `/Users/simon/gar/internal/agentsession` to `/Users/simon/gar/internal/agent/session`.
- Split tools into:
  - shared base tools: `/Users/simon/gar/internal/agent/tool`
  - coding-only tools/composition: `/Users/simon/gar/internal/coding-agent/tool`
- Introduce `/Users/simon/gar/internal/agentapp` as shared interactive runtime orchestration layer.
- Keep slash-command system centralized in `agentapp` (single owner), with capability checks per agent.

## 2. Non-Goals

- No provider/protocol redesign in `/Users/simon/gar/internal/llm`.
- No behavior expansion for tool semantics beyond current functionality.
- No deepresearch-agent feature implementation in this change.

## 3. Approaches

### Approach A: Keep app logic in `/internal/tui`, only move packages

- Move `tools` + `agentsession`; leave `app.go` as-is.
- Pros: lowest immediate churn.
- Cons: `tui` remains business-coupled to coding runtime and blocks clean multi-agent reuse.

### Approach B: Shared runtime shell (`agentapp`) + shared/public contracts (Recommended)

- Keep UI components in `tui`; move app orchestration and command handling to `agentapp`.
- `agentapp` owns unified commands and agent capability gating.
- Pros: clean boundaries, easiest to add `deepresearch-agent` without forking `tui`.
- Cons: medium refactor cost now.

### Approach C: Plugin-based everything now

- Build dynamic runtime plugin model for commands/tools/session controllers.
- Pros: maximal extensibility.
- Cons: over-engineered for current project phase, high risk and schedule drag.

Recommendation: Approach B.

## 4. Target Package Layout

- `/Users/simon/gar/internal/agent`
  - Shared model/tool loop core and queue semantics.
- `/Users/simon/gar/internal/agent/session`
  - Shared session tree, branch switching, compaction, queue snapshots, store integration.
- `/Users/simon/gar/internal/agent/tool`
  - Shared registry, shared tool implementations (`read/write/edit/bash/find/grep/ls`), shared helpers.
- `/Users/simon/gar/internal/agentapp`
  - Shared app orchestration (input routing, stream handling, slash command routing, selectors, status transitions).
- `/Users/simon/gar/internal/tui`
  - Pure UI components and rendering primitives (`chat`, `input`, `status`, `inspector`, `theme`).
- `/Users/simon/gar/internal/coding-agent/tool`
  - Coding-specific tool composition and coding-only tools (if any).

## 5. Dependency Rules

- `agent` may depend on shared `agent/tool` contracts but not on `coding-agent/*`.
- `agentapp` depends on `agent`, `agent/session`, shared `tui` components, and agent capability adapters.
- `coding-agent/*` may depend on shared layers.
- `tui` must not depend on `coding-agent/*` or `agent/session` directly.

## 6. What Moves Out Of `/internal/tui/app.go`

The following are runtime/business orchestration concerns and must move to `agentapp`:

- Session construction and wiring currently in `NewApp`.
- Submit vs queue semantics (`Submit`, `QueueSteer`, `QueueFollowUp`).
- Unified slash command implementation and validation.
- Session/tree selector data preparation and confirmation actions.
- Stream event persistence and runtime-state mutation (`RecordEvent`, tool-use intermediate handling).
- Session replay and session-status synchronization.

The following stay in `tui` as shared UI primitives:

- Panel layout + rendering composition.
- Chat viewport scrolling mechanics.
- Reusable input/status/inspector rendering components.

## 7. Slash Commands (Single Owner)

`agentapp` remains sole owner for command parsing, help text, and execution.

Supported set (centralized):
- `/help`
- `/session`
- `/name`
- `/new`
- `/resume`
- `/tree`
- `/branch`
- `/fork`
- `/compact`
- `/queue`
- `/dequeue`

For agents lacking a capability, `agentapp` returns deterministic capability errors.

## 8. Shared vs Agent-Specific Tool Split

Shared (`agent/tool`):
- `registry`
- `truncate`
- `workspace/path utils`
- `read`, `write`, `edit`, `bash`, `find`, `grep`, `ls`

Coding-specific (`coding-agent/tool`):
- shared bundle composition (`NewCodingTools`, `NewReadOnlyTools`, `NewAllTools`) and coding-only extensions.

## 9. Migration Safety Strategy

- Phase moves with package aliases avoided; update imports directly.
- Keep behavior parity via existing tests moved with packages.
- Add package-boundary tests for `agentapp` command routing and capability gating.
- Run full regression (`go test ./...`) after each migration phase.

## 10. Risks And Mitigations

- Risk: import cycles (`agentapp` <-> `tui` <-> runtime).
  - Mitigation: strict one-way dependency rules and interface seams.
- Risk: command behavior drift during move.
  - Mitigation: golden tests on command output and state transitions.
- Risk: deepresearch requirements differ later.
  - Mitigation: capability interfaces in `agentapp`, no coding-specific types in shared contracts.
