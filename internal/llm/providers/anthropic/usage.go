package anthropicprovider

import anthropic "github.com/anthropics/anthropic-sdk-go"
import "gar/internal/llm/core"

// applyStartUsage maps message_start usage counters to canonical usage fields.
func applyStartUsage(dst *core.Usage, usage anthropic.Usage) {
	dst.InputTokens = int(usage.InputTokens)
	dst.OutputTokens = int(usage.OutputTokens)
	dst.CacheReadTokens = int(usage.CacheReadInputTokens)
	dst.CacheWriteTokens = int(usage.CacheCreationInputTokens)
}

// applyDeltaUsage maps message_delta usage counters to canonical usage fields.
func applyDeltaUsage(dst *core.Usage, usage anthropic.MessageDeltaUsage) {
	dst.InputTokens = int(usage.InputTokens)
	dst.OutputTokens = int(usage.OutputTokens)
	dst.CacheReadTokens = int(usage.CacheReadInputTokens)
	dst.CacheWriteTokens = int(usage.CacheCreationInputTokens)
}
