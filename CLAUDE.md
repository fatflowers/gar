# CLAUDE.md — gar (Go Agent Runtime)

## Project Overview

gar is a minimal TUI-based coding agent built in Go. It focuses on transparency and learnability — users can see exactly what the agent is doing, why, and at what cost. Single binary, zero config, Anthropic-first with planned support for DeepSeek, OpenAI, and Ollama.

**Core philosophy: less is more.** Every feature must justify its existence. When in doubt, don't add it.

## Reference: pi-mono (badlogic/pi-mono)

gar's design is inspired by [pi-mono](https://github.com/badlogic/pi-mono), a TypeScript monorepo by Mario Zechner. Study pi-mono for design intent, but always translate to idiomatic Go. **Never port TypeScript patterns directly.**

### pi-mono's Core Design Principles (adopt these)

1. **Minimal system prompt** — Pi's system prompt is under 1000 tokens. No bloated instructions. The model already knows how to be a coding agent through RL training. gar follows the same principle: short identity + tool descriptions + AGENTS.md content, nothing more.

2. **Four tools only** — read, write, edit, bash. That's all you need. Models understand these tools inherently. Don't add grep/find/ls as separate tools — bash can do those. gar uses the same four-tool philosophy.

3. **YOLO by default** — No permission theater. If the agent can write and execute code, security prompts are security theater. gar runs unrestricted by default (with optional `--approve` flag for those who want confirmation).

4. **Progressive disclosure via AGENTS.md** — Project context is loaded hierarchically: global `~/.config/gar/agents.md` → project `.gar/agents.md` → subdirectory AGENTS.md files. Only inject context when relevant.

5. **Structured split tool results** — Tools return both LLM-facing content (text for the model) and UI-facing details (structured data for the TUI). This is a key insight from pi-mono.

6. **Session as append-only DAG** — Sessions are append-only JSONL with parent IDs, forming a tree structure that supports branching and replay. Each entry has a unique ID and optional parentId.

7. **Abort everywhere** — Every async operation (LLM streaming, tool execution) must respect context cancellation. Partial results should be preserved, not discarded.

8. **Event-driven UI** — The agent loop emits events (text_delta, tool_call_start, tool_call_end, turn_start, turn_end, etc.). UI subscribes to events, never polls.

9. **Transport independence** — The agent doesn't know how messages reach the LLM. A `StreamFunc` abstraction allows direct calls, proxy routing, or mock responses.

10. **No sub-agents, no plan mode, no MCP** (for now) — These add complexity without proportional value. If needed, users can spawn `gar --print` as a subprocess for sub-agent behavior.

### pi-mono Key Source Files to Study

| pi-mono file | What to learn | gar equivalent |
|---|---|---|
| `packages/ai/src/providers/anthropic.ts` | SSE streaming, tool_use parsing, token tracking | `internal/llm/anthropic.go` |
| `packages/ai/src/agent/agent-loop.ts` | Core loop: prompt → LLM → tool → repeat | `internal/agent/loop.go` |
| `packages/agent/src/agent.ts` | Agent state machine, message queuing (steer/followUp), abort | `internal/agent/agent.go` |
| `packages/coding-agent/src/tools/` | Tool implementations and truncation helpers | `internal/agent/tool/` + `internal/coding-agent/tool/` |
| `packages/coding-agent/src/core/agent-session.ts` | Session management, context building, compaction | `internal/agent/session/session.go` |
| `packages/tui/src/` | TUI rendering, differential updates | `internal/tui/` (use BubbleTea instead) |

### TypeScript → Go Translation Rules

**Do this, not that:**

| pi-mono (TypeScript) | gar (Go) | Why |
|---|---|---|
| `interface Provider { stream(...): AsyncGenerator<Event> }` | `type Provider interface { Stream(ctx context.Context, req *Request) (<-chan Event, error) }` | Go uses channels for streaming, not async generators |
| `class Agent { private state: AgentState }` | `type Agent struct { state agentState; mu sync.Mutex }` | Go has no classes. Use structs + methods. Protect mutable state with mutex |
| `agent.on('tool_call', callback)` | `type EventHandler interface { OnToolCall(tc ToolCall) }` or callback func | Go prefers interfaces or function types over event emitter patterns |
| `export interface AgentTool<TParams, TDetails>` | `type Tool interface { Name() string; Execute(ctx, params) (Result, error) }` | Go uses small interfaces, not generics-heavy abstractions |
| `async/await` throughout | `goroutine + channel + context.Context` | Go concurrency model. Use `ctx.Done()` for cancellation |
| TypeBox JSON Schema + AJV validation | `json.RawMessage` schema + manual or `go-jsonschema` validation | Keep it simple. Validate in tool execution, not framework |
| `type CustomAgentMessages = ...` declaration merging | Concrete `Message` struct with `Type` field + `json.RawMessage` payload | Go doesn't have declaration merging. Use a discriminated union via type field |
| npm monorepo with 7 packages | Single Go module with `internal/` packages | Go convention: single repo, internal packages for encapsulation |
| `import { ... } from '@mariozechner/pi-ai'` | `import "github.com/xxx/gar/internal/llm"` | Go import paths. All internal — no public library packages in v0.x |
| Ink/React-style TUI or custom retained-mode TUI | BubbleTea (Elm Architecture: Model → Update → View) | BubbleTea is Go's de facto TUI framework. Don't build a custom TUI framework |
| `EventEmitter` pattern for agent events | `chan Event` or callback interfaces | Channels are Go's native event primitive |
| `Promise.all` for concurrent operations | `errgroup.Group` from `golang.org/x/sync` | Structured concurrency with error propagation |
| Vercel AI SDK (explicitly avoided by pi-mono) | Also avoid heavy SDKs. Write thin HTTP client | Same philosophy: direct HTTP for full control |

### Pi-mono Patterns to Adapt Carefully

**Message queuing (steer / followUp):**
Pi-mono has two message queues: steering (interrupts current tool) and follow-up (waits for turn end). In Go, implement this with buffered channels and select:

```go
type Agent struct {
    steerCh    chan Message  // checked after each tool execution
    followUpCh chan Message  // checked after turn ends
}

// In the agent loop:
select {
case msg := <-a.steerCh:
    // interrupt: skip remaining tools, inject this message
case <-ctx.Done():
    // cancelled
default:
    // continue normal execution
}
```

**Tool result splitting (LLM content vs UI details):**
Pi-mono returns `{ output: string, details: T }` from tools. In Go:

```go
type ToolResult struct {
    Content string      // sent to LLM as tool_result
    Display DisplayData // sent to TUI for rendering (not sent to LLM)
    Error   error
}

type DisplayData struct {
    Type    string          // "file_content", "bash_output", "diff", etc.
    Payload json.RawMessage // structured data for TUI rendering
}
```

**SSE streaming with partial tool call parsing:**
Pi-mono progressively parses JSON tool arguments during streaming. In Go, use a streaming JSON decoder or accumulate chunks:

```go
// Accumulate tool call JSON fragments
type toolCallAccumulator struct {
    id     string
    name   string
    argBuf strings.Builder // append each content_block_delta
}

// When complete, unmarshal:
var params json.RawMessage
json.Unmarshal([]byte(acc.argBuf.String()), &params)
```

**Context handoff between providers:**
Pi-mono transforms provider-specific message formats (thinking blocks, signed blobs) when switching models. For gar v0.1, this is not needed (single provider). When adding multi-provider in v0.2, define a canonical internal message format and transform at the provider boundary:

```go
// internal/llm/message.go — canonical format
type Message struct { Role, Content, ToolCalls, ThinkingBlocks, ... }

// internal/llm/anthropic.go — transforms to/from Anthropic wire format
func (p *AnthropicProvider) toWireMessages(msgs []Message) []anthropicMessage { ... }
func (p *AnthropicProvider) fromWireMessage(raw anthropicAssistantMsg) Message { ... }
```

### Pi-mono Patterns to NOT Adopt

| pi-mono pattern | Why not in gar |
|---|---|
| TypeScript monorepo with 7 packages | Over-engineered for a single Go binary. Use `internal/` packages |
| Custom TUI framework (pi-tui) | BubbleTea exists and is mature. Don't reinvent |
| TypeBox schemas for tool parameters | Go's `json.RawMessage` + struct tags are simpler |
| npm package system for extensions | Go plugins are fragile. Use CLI tool + README pattern (which pi-mono itself recommends over MCP) |
| `jiti` runtime TypeScript compilation for extensions | Not applicable. If plugins needed later, use Go plugin or hashicorp/go-plugin |
| OAuth for Claude Pro/Max subscriptions | Not needed for API-key-based access |
| Web UI package (pi-web-ui) | Terminal-first. Web UI is v0.4+ at earliest |
| vLLM pod management (pi-pods) | Out of scope entirely |

## Architecture

```
cmd/gar/main.go          → CLI entry point (cobra)
internal/
├── llm/                  → LLM provider abstraction
│   ├── provider.go       → Provider interface + Event types
│   ├── anthropic.go      → Anthropic Claude implementation (SSE streaming)
│   └── message.go        → Message, Role, ToolCall, Usage types
├── agent/                → Shared agent runtime
│   ├── agent.go          → Agent struct, public API (Run, Cancel, State)
│   ├── loop.go           → Main run loop: prompt → LLM → tool → loop
│   ├── state.go          → AgentState enum (Idle/Streaming/ToolExecuting/Error)
│   ├── session/          → Shared session tree/compaction runtime
│   └── tool/             → Shared base tools + registry
├── agentapp/             → Shared app orchestration + slash commands
├── coding-agent/         → Coding-agent specific composition
│   └── tool/             → Coding tool bundles and coding-only tools
├── tui/                  → BubbleTea TUI layer
│   ├── app.go            → Root bubbletea.Model, orchestrates all views
│   ├── chat.go           → Message stream viewport
│   ├── input.go          → Multi-line input editor
│   ├── inspector.go      → Agent transparency panel (tokens, cost, state)
│   ├── status.go         → Header/footer status bar
│   └── theme.go          → Color themes (dark/light)
├── session/              → Session persistence
│   └── session.go        → JSONL-based session save/load/list
└── config/               → Configuration
    └── config.go         → TOML config + env var + flag precedence
```

### Dependency Direction (strict, no cycles)

```
cmd/gar → internal/tui → internal/agent → internal/llm
                       → internal/agent/session
                       → internal/agent/tool
                       → internal/agentapp
                       → internal/coding-agent/tool
                       → internal/session
                       → internal/config
```

- `llm/` has ZERO internal dependencies — it is the foundation layer
- `agent/tool/` depends mostly on stdlib and shared model types
- `agent/` depends on `llm/` and shared tool contracts
- `tui/` depends on everything else (it is the top-level orchestrator)
- `config/` has ZERO internal dependencies

## Commands

```bash
# Development
go build ./cmd/gar           # Build
go run ./cmd/gar             # Run from source
go test ./...                # Run all tests
go test ./internal/llm/...   # Test specific package
go test -run TestAgentLoop   # Run specific test
go vet ./...                 # Static analysis
golangci-lint run            # Linting (if installed)

# Release
goreleaser release --snapshot --clean   # Test release locally
```

## Coding Conventions

### Go Style

- **Go 1.26** — use range-over-int, range-over-func, slices/maps packages, enhanced servemux patterns, etc.
- Follow standard Go conventions: `gofmt`, `go vet`, effective Go
- **No global state** — pass dependencies explicitly via constructors
- **Errors are values** — return `error`, never panic in library code. Use `fmt.Errorf("operation: %w", err)` for wrapping
- **Context everywhere** — all I/O functions take `context.Context` as first parameter
- **Interfaces at consumer side** — define interfaces where they are used, not where they are implemented
- **Small interfaces** — prefer 1-2 method interfaces. `io.Reader` over `ReadWriteCloserSeeker`
- **Table-driven tests** — use `[]struct{ name string; ... }` pattern for test cases
- **No `init()`** — explicit initialization only
- **Unexported by default** — only export what is part of the public API

### Naming

- Package names: short, lowercase, singular (`llm`, `agent`, `tools`, `tui`)
- Files: lowercase, snake_case (`agent_loop.go`, `tool_read.go`)
- Interfaces: `-er` suffix when natural (`Provider`, `Tool`), otherwise descriptive nouns
- Constructors: `New` prefix (`NewAgent`, `NewAnthropicProvider`)
- Options: functional options pattern for complex constructors

### Key Interfaces

```go
// llm/provider.go
type Provider interface {
    Stream(ctx context.Context, req *Request) (<-chan Event, error)
}

// agent/tool/registry.go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, params json.RawMessage) (string, error)
}
```

### Error Handling Patterns

```go
// Wrap with context
if err != nil {
    return fmt.Errorf("read file %s: %w", path, err)
}

// Sentinel errors for expected conditions
var ErrSessionNotFound = errors.New("session not found")

// Never ignore errors silently. If intentionally ignoring, comment why:
_ = conn.Close() // best-effort cleanup, error not actionable
```

### Concurrency Patterns

- Use channels for streaming data (LLM events → TUI)
- Use `context.Context` for cancellation propagation
- Use `sync.WaitGroup` for goroutine lifecycle management
- BubbleTea uses the Elm Architecture (Msg → Update → View), send messages via `tea.Cmd`
- **Never block the TUI Update loop** — all I/O in `tea.Cmd` functions

## LLM Integration Notes

### Anthropic API

- Use SSE streaming endpoint (`/v1/messages` with `stream: true`)
- Parse `content_block_delta`, `content_block_start`, `message_delta` events
- Tool use: Claude returns `tool_use` content blocks → execute locally → send `tool_result` back
- Token tracking: extract from `message_delta.usage` event (input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens)
- Cost calculation: based on model pricing, update in `llm/pricing.go`
- Handle rate limits with exponential backoff
- Set `max_tokens` explicitly (required by Anthropic API)

### System Prompt Assembly

Pi-mono's key insight: the system prompt should be **under 1000 tokens**. Models are already RL-trained to be coding agents. Don't over-instruct.

Order of assembly:

1. Base identity (2-3 sentences): "You are gar, an expert coding assistant. You help users by reading files, executing commands, editing code, and writing new files."
2. Tool descriptions (auto-generated from Tool.Description() — keep each under 100 words)
3. Brief guidelines: "Use bash for file operations like ls, grep, find. Use read to examine files before editing. Use edit for precise changes. Use write only for new files or complete rewrites. Be concise."
4. Working directory info: cwd, git branch (if in a git repo)
5. AGENTS.md / CLAUDE.md content (if found — scan from project root upward)

**Do NOT add:** lengthy examples, safety disclaimers, output formatting rules, step-by-step instructions. The model doesn't need them.

### Tool Call Flow

Following pi-mono's pattern: tools return split results (LLM content + UI display data).

```
LLM returns tool_use block
    → Find tool in registry by name
    → Validate params against schema (if invalid, return error to LLM for retry)
    → Check approval policy (auto / ask / deny)
    → If ask: send ToolApprovalRequest to TUI, wait for user response
    → Execute tool with context (respect cancellation via ctx.Done())
    → Tool returns ToolResult { Content string, Display DisplayData, Error error }
    → Content → sent to LLM as tool_result
    → Display → sent to TUI for rich rendering (not sent to LLM)
    → Truncate Content if > 10K chars (keep first 4K + last 4K + "[truncated]")
    → LLM continues (may call more tools or produce text)
```

## TUI Architecture

BubbleTea Elm Architecture:

- **Model**: `App` struct holds all state (agent, messages, input, inspector data)
- **Update**: handle keyboard events, agent events (via tea.Msg), resize
- **View**: render chat + input + inspector + status bar using lipgloss

### Key Messages (tea.Msg types)

```go
type StreamTextMsg string           // LLM text delta
type ToolCallMsg struct{ ... }      // Tool call started
type ToolResultMsg struct{ ... }    // Tool execution completed
type ToolApprovalMsg struct{ ... }  // Ask user to approve tool
type AgentDoneMsg struct{}          // Agent turn complete
type AgentErrorMsg struct{ Err error }
type TokenUpdateMsg struct{ ... }   // Token/cost update
```

### Layout

```
┌──────────────────────────────────────────────────────────────┐
│ gar v0.1.0 │ claude-sonnet-4 │ ~/project │ session: abc123  │ ← status bar
├──────────────────────────────────────┬───────────────────────┤
│                                      │ Status: Streaming     │
│  Chat messages viewport              │ Turn: 3               │
│  (scrollable, markdown rendered)     │ Tokens: 12.4K/200K   │
│                                      │ Cost: $0.042          │
│                                      │ Context: ████░░ 62%  │
│                                      │ Tools called:         │
│                                      │   ReadFile (2)        │
│                                      │   Bash (1)            │
├──────────────────────────────────────┴───────────────────────┤
│ > multi-line input area                              Ctrl+⏎  │ ← input editor
└──────────────────────────────────────────────────────────────┘
```

## Session Format

JSONL file, one entry per line:

```jsonl
{"id":"01","type":"meta","data":{"model":"claude-sonnet-4","cwd":"/home/user/project"},"ts":1234567890}
{"id":"02","type":"user","content":"read main.go","ts":1234567891}
{"id":"03","type":"tool_call","name":"ReadFile","params":{"path":"main.go"},"ts":1234567892}
{"id":"04","type":"tool_result","tool_call_id":"03","content":"package main...","ts":1234567893}
{"id":"05","type":"assistant","content":"This file contains...","ts":1234567894,"usage":{"in":1234,"out":567}}
```

Sessions stored in `.gar/sessions/` relative to project root.

## Configuration

Precedence: CLI flags > environment variables > config file > defaults

```toml
# ~/.config/gar/config.toml

[provider]
default = "anthropic"

[provider.anthropic]
api_key = ""                      # or ANTHROPIC_API_KEY env var
model = "claude-sonnet-4-20250514"

[agent]
auto_approve = ["ReadFile"]       # tools that skip approval
max_turns = 50
thinking_level = "medium"

[tui]
theme = "dark"
show_inspector = true
```

## Testing Strategy

- **Unit tests**: all packages under `internal/` have `_test.go` files
- **LLM tests**: gated behind `GAR_TEST_LLM=1` env var (skipped by default)
- **TUI tests**: use bubbletea's `teatest` package for headless testing
- **Integration tests**: full agent loop with mock provider (returns scripted responses)
- Mock provider in `llm/mock.go` for deterministic testing
- Use `testdata/` directories for fixture files

## Git Conventions

- Commit messages: conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`)
- Branch naming: `feat/xxx`, `fix/xxx`, `docs/xxx`
- Keep commits atomic — one logical change per commit
- Always run `go test ./...` and `go vet ./...` before committing

## Things to Avoid

Pi-mono's philosophy: "if I don't need it, it won't be built." Apply this ruthlessly.

- **No premature abstraction** — don't add a plugin system until v0.3+
- **No sub-agents** — if needed, spawn `gar --print` as subprocess (pi-mono recommends this too)
- **No plan mode** — pi-mono explicitly skips this; models handle planning internally
- **No MCP** — pi-mono avoids MCP; prefer CLI tools + README (bash can invoke anything)
- **No built-in web search/fetch tools** — bash + curl covers this
- **No background bash** — pi-mono intentionally keeps bash synchronous. No process management complexity
- **No permission security theater** — YOLO by default (like pi-mono). Don't waste tokens on Haiku pre-checking commands
- **No web UI** until TUI is polished
- **No database** — JSONL is enough for v0.1-v0.2
- **No third-party LLM SDKs** if they add bloat — prefer thin HTTP wrappers (pi-mono avoids Vercel AI SDK for the same reason)
- **No `interface{}` / `any`** — use concrete types or generics
- **No channels where a mutex suffices** — don't over-complicate concurrency
- **No vendor directory** — use Go modules normally
- **No bloated system prompts** — pi-mono keeps it under 1000 tokens total. So should gar

## Bilingual Documentation

- `README.md` — English (primary for GitHub discoverability)
- `README_CN.md` — Chinese (中文)
- Code comments: English
- Commit messages: English
- Blog posts: publish both languages simultaneously
