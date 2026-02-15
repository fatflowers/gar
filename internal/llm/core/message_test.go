package core

import "testing"

// TestStopReasonValues ensures all public stop reason constants remain wired.
func TestStopReasonValues(t *testing.T) {
	cases := []StopReason{
		StopReasonStop,
		StopReasonLength,
		StopReasonToolUse,
		StopReasonError,
		StopReasonAborted,
	}
	if len(cases) != 5 {
		t.Fatalf("unexpected stop reason count: %d", len(cases))
	}
}

// TestEventTypeValues ensures all public event type constants remain wired.
func TestEventTypeValues(t *testing.T) {
	cases := []EventType{
		EventStart,
		EventContentBlockStart,
		EventTextDelta,
		EventToolCallStart,
		EventToolCallDelta,
		EventToolCallEnd,
		EventUsage,
		EventDone,
		EventError,
	}
	if len(cases) != 9 {
		t.Fatalf("unexpected event type count: %d", len(cases))
	}
}

func TestUsageTokenCount(t *testing.T) {
	t.Parallel()

	usage := Usage{
		InputTokens:      10,
		OutputTokens:     7,
		CacheReadTokens:  5,
		CacheWriteTokens: 3,
	}
	if got := usage.TokenCount(); got != 25 {
		t.Fatalf("TokenCount() = %d, want 25", got)
	}
}

func TestUsageCloneReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	usage := Usage{
		InputTokens:      2,
		OutputTokens:     3,
		CacheReadTokens:  4,
		CacheWriteTokens: 5,
		TotalTokens:      14,
		CostUSD:          0.01,
	}
	cloned := usage.Clone()
	if cloned == nil {
		t.Fatalf("Clone() returned nil")
	}
	if *cloned != usage {
		t.Fatalf("Clone() value mismatch: got %#v want %#v", *cloned, usage)
	}

	cloned.InputTokens = 99
	if usage.InputTokens != 2 {
		t.Fatalf("mutating clone should not mutate original: original=%#v clone=%#v", usage, *cloned)
	}
}
