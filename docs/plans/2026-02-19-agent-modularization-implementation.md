# Agent Modularization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reorganize runtime code so shared agent infrastructure lives in reusable packages and coding-specific modules live under `internal/coding-agent`, while preserving current behavior.

**Architecture:** Move session and base tools into shared namespaces (`internal/agent/session`, `internal/agent/tool`), introduce `internal/agentapp` as shared runtime orchestration with centralized slash commands, and keep `internal/tui` as pure UI components. Add a thin coding-agent composition layer for coding-specific tool bundles and wiring.

**Tech Stack:** Go 1.26, BubbleTea, existing `internal/llm`, current JSONL session store, `go test`.

---

### Task 1: Create Shared Runtime Skeleton

**Files:**
- Create: `/Users/simon/gar/internal/agent/session/.keep`
- Create: `/Users/simon/gar/internal/agent/tool/.keep`
- Create: `/Users/simon/gar/internal/agentapp/.keep`

**Step 1: Create package folders**

Run: `mkdir -p /Users/simon/gar/internal/agent/session /Users/simon/gar/internal/agent/tool /Users/simon/gar/internal/agentapp`
Expected: directories exist.

**Step 2: Add placeholder files to make tree explicit in diff**

Run: `touch /Users/simon/gar/internal/agent/session/.keep /Users/simon/gar/internal/agent/tool/.keep /Users/simon/gar/internal/agentapp/.keep`
Expected: git tracks new directories.

**Step 3: Commit**

Run:
```bash
git add /Users/simon/gar/internal/agent/session/.keep /Users/simon/gar/internal/agent/tool/.keep /Users/simon/gar/internal/agentapp/.keep
git commit -m "chore: add shared runtime package skeleton"
```

### Task 2: Move Session Package To Shared Namespace

**Files:**
- Move: `/Users/simon/gar/internal/agentsession/session.go` -> `/Users/simon/gar/internal/agent/session/session.go`
- Move: `/Users/simon/gar/internal/agentsession/session_test.go` -> `/Users/simon/gar/internal/agent/session/session_test.go`
- Modify imports in:
  - `/Users/simon/gar/internal/tui/app.go`
  - `/Users/simon/gar/internal/tui/app_test.go`
  - any file importing `gar/internal/agentsession`

**Step 1: Move files and rename package from `agentsession` to `session`**

Edit package declarations and import paths.

**Step 2: Run failing compile check to catch stale imports**

Run: `go test ./...`
Expected: FAIL with unresolved `internal/agentsession` references.

**Step 3: Fix remaining imports**

Replace `gar/internal/agentsession` with `gar/internal/agent/session`.

**Step 4: Run focused tests**

Run: `go test /Users/simon/gar/internal/agent/session`
Expected: PASS.

**Step 5: Run full tests**

Run: `go test ./...`
Expected: PASS.

**Step 6: Commit**

Run:
```bash
git add /Users/simon/gar/internal/agent/session /Users/simon/gar/internal/tui/app.go /Users/simon/gar/internal/tui/app_test.go
git commit -m "refactor: move agentsession to internal/agent/session"
```

### Task 3: Move Shared Base Tools To `internal/agent/tool`

**Files:**
- Move all shared tool runtime files from `/Users/simon/gar/internal/tools` to `/Users/simon/gar/internal/agent/tool`:
  - `registry.go`, `params.go`, `truncate.go`, `workspace.go`, `glob.go`, `edit_diff.go`
  - `read.go`, `write.go`, `edit.go`, `bash.go`, `find.go`, `grep.go`, `ls.go`
  - related tests except coding-specific catalog tests.
- Keep coding-only composition for later task.

**Step 1: Move files and rename package from `tools` to `tool`**

Update all `package` declarations and internal references.

**Step 2: Add failing compile check**

Run: `go test ./internal/agent/tool`
Expected: FAIL initially for unresolved package names/imports.

**Step 3: Fix compile errors incrementally**

Update imports and symbol references until package compiles.

**Step 4: Run focused tests**

Run: `go test ./internal/agent/tool`
Expected: PASS.

**Step 5: Run full tests**

Run: `go test ./...`
Expected: PASS.

**Step 6: Commit**

Run:
```bash
git add /Users/simon/gar/internal/agent/tool /Users/simon/gar/internal/tools
git commit -m "refactor: move shared tools to internal/agent/tool"
```

### Task 4: Extract Agent Tool Execution Interface (Decouple Shared Agent Core)

**Files:**
- Create: `/Users/simon/gar/internal/agent/toolkit.go`
- Modify: `/Users/simon/gar/internal/agent/agent.go`
- Modify: `/Users/simon/gar/internal/agent/agent_test.go`

**Step 1: Write failing test for interface-based tool execution**

Add a fake toolkit type in `agent_test.go` and assert `agent.Config` accepts it without importing concrete tool package.

**Step 2: Run targeted test and verify red**

Run: `go test ./internal/agent -run Tool`
Expected: FAIL due missing interface wiring.

**Step 3: Add shared interface**

In `toolkit.go`:
```go
package agent

import (
    "context"
    "encoding/json"
)

type ToolResult struct {
    Content string
}

type ToolExecutor interface {
    Execute(ctx context.Context, name string, params json.RawMessage) (ToolResult, error)
}
```

**Step 4: Refactor `agent.Config` and execution path**

Use `ToolExecutor` in `agent.Config` and runtime instead of concrete registry type.

**Step 5: Re-run targeted and full tests**

Run:
- `go test ./internal/agent -run Tool`
- `go test ./...`
Expected: PASS.

**Step 6: Commit**

Run:
```bash
git add /Users/simon/gar/internal/agent/toolkit.go /Users/simon/gar/internal/agent/agent.go /Users/simon/gar/internal/agent/agent_test.go
git commit -m "refactor: decouple agent core from concrete tool registry"
```

### Task 5: Create Coding-Agent Tool Composition Layer

**Files:**
- Create: `/Users/simon/gar/internal/coding-agent/tool/catalog.go`
- Create: `/Users/simon/gar/internal/coding-agent/tool/catalog_test.go`
- Delete or migrate: `/Users/simon/gar/internal/tools/catalog.go`
- Delete or migrate: `/Users/simon/gar/internal/tools/catalog_test.go`

**Step 1: Write failing tests for coding tool bundles**

Add tests for:
- coding bundle contains `read/bash/edit/write`
- read-only bundle contains `read/grep/find/ls`

**Step 2: Run focused tests and verify red**

Run: `go test ./internal/coding-agent/tool`
Expected: FAIL before implementation.

**Step 3: Implement composition using shared tools**

`catalog.go` returns `[]tool.Tool` from `/Users/simon/gar/internal/agent/tool` constructors.

**Step 4: Run focused and full tests**

Run:
- `go test ./internal/coding-agent/tool`
- `go test ./...`
Expected: PASS.

**Step 5: Commit**

Run:
```bash
git add /Users/simon/gar/internal/coding-agent/tool /Users/simon/gar/internal/tools/catalog.go /Users/simon/gar/internal/tools/catalog_test.go
git commit -m "feat: add coding-agent tool composition layer"
```

### Task 6: Introduce Shared AgentApp Command Runtime

**Files:**
- Create: `/Users/simon/gar/internal/agentapp/types.go`
- Create: `/Users/simon/gar/internal/agentapp/commands.go`
- Create: `/Users/simon/gar/internal/agentapp/commands_test.go`

**Step 1: Write failing tests for centralized commands**

Test `/help`, `/session`, `/name`, `/new`, `/resume`, `/tree`, `/branch`, `/fork`, `/compact`, `/queue`, `/dequeue` routing through one command dispatcher.

**Step 2: Run targeted tests and verify red**

Run: `go test ./internal/agentapp -run Command`
Expected: FAIL before command runtime exists.

**Step 3: Implement capability interfaces and command router**

Define interfaces for:
- session lifecycle
- branch/tree navigation
- queue operations
- chat output sink

Implement one parser/dispatcher in `commands.go`.

**Step 4: Re-run targeted tests**

Run: `go test ./internal/agentapp -run Command`
Expected: PASS.

**Step 5: Commit**

Run:
```bash
git add /Users/simon/gar/internal/agentapp
git commit -m "feat: add shared agentapp command runtime"
```

### Task 7: Refactor TUI App To Use AgentApp (Keep TUI Shared)

**Files:**
- Modify: `/Users/simon/gar/internal/tui/app.go`
- Modify: `/Users/simon/gar/internal/tui/app_test.go`

**Step 1: Write failing integration tests for app-to-agentapp delegation**

Add tests verifying slash commands execute via `agentapp` abstraction, not inlined switch logic in `tui/app.go`.

**Step 2: Run targeted tests and verify red**

Run: `go test ./internal/tui -run Slash`
Expected: FAIL before refactor.

**Step 3: Move inlined slash logic out of app.go**

Replace command switch with delegation into `agentapp` command runtime.

**Step 4: Keep rendering-only behavior in `tui`**

Ensure `renderBody`, input rendering, scrolling, and panel composition remain in `tui`.

**Step 5: Run targeted + full tests**

Run:
- `go test ./internal/tui`
- `go test ./...`
Expected: PASS.

**Step 6: Commit**

Run:
```bash
git add /Users/simon/gar/internal/tui/app.go /Users/simon/gar/internal/tui/app_test.go
git commit -m "refactor: delegate app orchestration to shared agentapp"
```

### Task 8: Wire Main Entry To New Package Graph

**Files:**
- Modify: `/Users/simon/gar/cmd/gar/main.go`

**Step 1: Write failing compile assertion via test run**

Run: `go test ./cmd/gar`
Expected: FAIL if imports/stitching still point to old paths.

**Step 2: Update wiring**

- Use shared tool registry from `/Users/simon/gar/internal/agent/tool`.
- Use coding composition from `/Users/simon/gar/internal/coding-agent/tool`.
- Ensure app/session imports are the new namespaces.

**Step 3: Run command package + full tests**

Run:
- `go test ./cmd/gar`
- `go test ./...`
Expected: PASS.

**Step 4: Commit**

Run:
```bash
git add /Users/simon/gar/cmd/gar/main.go
git commit -m "refactor: wire cmd/gar to modular agent packages"
```

### Task 9: Remove Obsolete Paths And Enforce Boundaries

**Files:**
- Delete: `/Users/simon/gar/internal/agentsession` (old path)
- Delete: `/Users/simon/gar/internal/tools` (old path)
- Create: `/Users/simon/gar/internal/agentapp/README.md` (boundary docs)

**Step 1: Delete deprecated directories and stale imports**

Remove old package directories after all imports are migrated.

**Step 2: Add architecture boundary doc**

Document allowed imports and package responsibilities.

**Step 3: Verify no stale imports**

Run: `rg -n "internal/agentsession|internal/tools" /Users/simon/gar`
Expected: no runtime references remaining.

**Step 4: Full verification**

Run:
- `go test ./...`
- `go vet ./...`
Expected: PASS.

**Step 5: Commit**

Run:
```bash
git add -A
git commit -m "chore: remove legacy package paths and document boundaries"
```

---

## Execution Notes

- Use `@superpowers:test-driven-development` for every behavior change.
- Use `@superpowers:verification-before-completion` before each completion claim.
- Keep commits small and task-scoped.

