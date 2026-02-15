package core

// ModelPricing is priced in USD per 1M tokens.
type ModelPricing struct {
	InputPerMTokUSD      float64
	OutputPerMTokUSD     float64
	CacheReadPerMTokUSD  float64
	CacheWritePerMTokUSD float64
}

// CalculateCost returns the USD cost for the usage snapshot.
func CalculateCost(u Usage, p ModelPricing) float64 {
	input := (float64(u.InputTokens) / 1_000_000.0) * p.InputPerMTokUSD
	output := (float64(u.OutputTokens) / 1_000_000.0) * p.OutputPerMTokUSD
	cacheRead := (float64(u.CacheReadTokens) / 1_000_000.0) * p.CacheReadPerMTokUSD
	cacheWrite := (float64(u.CacheWriteTokens) / 1_000_000.0) * p.CacheWritePerMTokUSD
	return input + output + cacheRead + cacheWrite
}
