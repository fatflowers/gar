# internal/llm Unified Design

Date: 2026-02-15
Status: Approved
Scope: Anthropic-first LLM layer with stable facade API and internal package split for maintainability.

## 1. Goals

- Keep external import and API stable at `gar/internal/llm`.
- Support Anthropic streaming as the current provider implementation.
- Provide provider-agnostic core models and stream events.
- Keep retry, usage, pricing, and tool schema handling reusable across providers.
- Make future provider additions low risk by isolating provider-specific code.

## 2. Non-Goals

- No public API redesign for existing `llm` callers.
- No additional provider behavior beyond current Anthropic and mock scope.
- No UI or cross-module feature changes outside `internal/llm`.

## 3. Target Architecture

- `internal/llm`
  - Facade only.
  - Re-exports core types/constants via type aliases.
  - Exposes stable constructors and helpers (for example `NewAnthropicProvider`, `NewToolSpecFromStruct`, `CalculateCost`).
- `internal/llm/core`
  - Provider-agnostic domain types:
    - request/message/event/usage/pricing/retry/errors/tool schema/json helpers.
  - Must not import provider packages.
- `internal/llm/providers/anthropic`
  - Anthropic SDK integration and stream mapping logic.
  - Anthropic-specific request mapping and retry classification.
- `internal/llm/providers/mock`
  - Deterministic mock stream provider for tests.

## 4. Dependency Rules

- `internal/llm` -> `internal/llm/core`
- `internal/llm` -> `internal/llm/providers/anthropic`
- `internal/llm` -> `internal/llm/providers/mock`
- `internal/llm/providers/*` -> `internal/llm/core`
- `internal/llm/core` does not import `internal/llm` or any provider package

This keeps dependencies one-way and avoids import cycles.

## 5. Public API Compatibility Strategy

- Keep `gar/internal/llm` as the only import path consumers need.
- Preserve exported names through aliases where possible:
  - `Provider`, `Request`, `Event`, `Message`, `Usage`, `RetryPolicy`, `ToolSpec`, `ToolChoice`, `StopReason`, and related constants.
- Keep facade helper functions for non-alias exports:
  - `NewAnthropicProvider`
  - `NewToolSpecFromStruct`
  - `CalculateCost`
- Keep provider SDK-specific types out of facade APIs.

## 6. Anthropic Provider Behavior

### Outbound Mapping

- Map canonical request into Anthropic Messages API params:
  - model, system, messages, tools, max tokens, temperature, tool choice, metadata.
- Convert messages:
  - user/assistant text blocks to Anthropic text blocks.
  - consecutive tool results to user message `tool_result` blocks.

### Streaming Event Mapping

Map Anthropic stream events to canonical events:

- `message_start` -> usage snapshot event.
- `content_block_start` -> `content_block_start` event.
  - supported block types:
    - `text`
    - `thinking`
    - `redacted_thinking`
    - `tool_use`
    - `server_tool_use`
    - `web_search_tool_result`
- `content_block_delta`:
  - text delta -> `text_delta`
  - input json delta -> `tool_call_delta` + incremental accumulator
- `content_block_stop`:
  - finalize tool call arguments and emit `tool_call_end`
- `message_delta`:
  - update stop reason and usage
- `message_stop`:
  - emit terminal `done`

## 7. Retry and Error Policy

- Default retry policy:
  - max retries: 3
  - base delay: 300ms
  - max delay: 5s
  - jittered exponential backoff
- Retry only before any visible output is emitted.
  - Prevents duplicate partial output after deltas/tool events.
- Retryable classes include:
  - 429 and 5xx API errors
  - transient network errors
- Cancellation and deadline:
  - respected throughout stream and backoff waits
  - surfaced as aborted terminal error semantics

## 8. Tool Schema Strategy

- Tool input schemas are generated from Go structs with `invopop/jsonschema`.
- Canonical schema normalization is performed in `core`.
- Anthropic provider consumes normalized schema for tool declaration mapping.

## 9. Validation Expectations

- Facade compatibility: existing callers compile unchanged against `gar/internal/llm`.
- Package-level compile checks pass:
  - `go test ./internal/llm/... -run '^$'`
- Workspace compile checks pass:
  - `go test ./... -run '^$'`
- Provider behavior remains consistent with existing tests for:
  - text stream
  - tool-call chunk assembly
  - retry before first visible delta
  - no retry after visible output
  - cancellation aborted semantics

