# Tools + Agent Loop Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the four built-in tools (`read`, `write`, `edit`, `bash`) plus a registry, then wire agent loop tool execution so `tool_use` can continue with `tool_result`.

**Architecture:** Keep tools in `internal/tools` with a small `Tool` interface and typed parameter decoding per tool. Extend `internal/agent` with optional tool registry dependency; when the model emits tool calls, execute them synchronously with context cancellation and append `tool_result` messages back into request history.

**Tech Stack:** Go 1.26, stdlib (`os`, `os/exec`, `context`, `encoding/json`, `path/filepath`, `strings`, `bytes`, `sync`), existing `internal/llm`.

---

### Task 1: Implement tool registry contract

**Files:**
- Modify: `internal/tools/registry.go`
- Test: `internal/tools/registry_test.go`

**Steps:**
1. Write failing tests for register/get/execute + duplicate name + unknown tool.
2. Run `go test ./internal/tools -run TestRegistry`.
3. Implement minimal `Tool` interface, `Result`, `Registry`, and exported sentinel errors.
4. Re-run `go test ./internal/tools -run TestRegistry`.

### Task 2: Implement read/write/edit/bash tools

**Files:**
- Modify: `internal/tools/read.go`
- Modify: `internal/tools/write.go`
- Modify: `internal/tools/edit.go`
- Modify: `internal/tools/bash.go`
- Test: `internal/tools/read_test.go`
- Test: `internal/tools/write_test.go`
- Test: `internal/tools/edit_test.go`
- Test: `internal/tools/bash_test.go`

**Steps:**
1. Add failing tests for each tool’s happy path + one core error path.
2. Run focused tests per file to confirm red.
3. Implement minimal behavior:
   - `read`: read file content from `path`.
   - `write`: write full content to file (create parent dirs).
   - `edit`: single string replacement, fail on no match or ambiguous match.
   - `bash`: run command with timeout and output truncation.
4. Re-run focused tests + `go test ./internal/tools`.

### Task 3: Wire tool execution into agent loop

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/loop.go`
- Test: `internal/agent/agent_test.go`

**Steps:**
1. Add failing tests showing `tool_use` runs tool and continues to next turn with `tool_result`.
2. Run `go test ./internal/agent -run ToolUse`.
3. Add optional tool registry dependency in `agent.Config` and pass execute hook into loop.
4. Implement loop behavior: on `StopReasonToolUse`, execute each tool call, append `tool_result` messages, then continue loop.
5. Re-run `go test ./internal/agent -run ToolUse` and full `go test ./internal/agent`.

### Task 4: End-to-end verification

**Files:**
- No file changes expected.

**Steps:**
1. Run `go test ./...`.
2. Run `go vet ./...`.
3. Record verification summary in handoff.
