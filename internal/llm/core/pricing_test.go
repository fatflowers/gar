package core

import (
	"math"
	"testing"
)

// TestCalculateCost verifies per-bucket token pricing is summed correctly.
func TestCalculateCost(t *testing.T) {
	usage := Usage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  200,
		CacheWriteTokens: 100,
	}
	pricing := ModelPricing{
		InputPerMTokUSD:      3.00,
		OutputPerMTokUSD:     15.00,
		CacheReadPerMTokUSD:  0.30,
		CacheWritePerMTokUSD: 3.75,
	}

	got := CalculateCost(usage, pricing)
	want := 0.003 + 0.0075 + 0.00006 + 0.000375
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("unexpected cost: got %f, want %f", got, want)
	}
}
