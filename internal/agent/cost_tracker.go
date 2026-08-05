package agent

import (
	"sync"
	"time"
)

// ModelPricing maps model names to their per-token costs in USD.
type ModelPricing struct {
	InputCostPer1K  float64 `json:"input_cost_per_1k"`
	OutputCostPer1K float64 `json:"output_cost_per_1k"`
}

// DefaultModelPricing returns the standard pricing table for common models.
func DefaultModelPricing() map[string]ModelPricing {
	return map[string]ModelPricing{
		"claude-opus-4-7":   {InputCostPer1K: 0.015, OutputCostPer1K: 0.075},
		"claude-sonnet-4-6": {InputCostPer1K: 0.003, OutputCostPer1K: 0.015},
		"claude-haiku-4-5":  {InputCostPer1K: 0.0008, OutputCostPer1K: 0.004},
		"gpt-4o":            {InputCostPer1K: 0.005, OutputCostPer1K: 0.015},
		"gpt-4o-mini":       {InputCostPer1K: 0.00015, OutputCostPer1K: 0.0006},
		"deepseek-v4-pro":   {InputCostPer1K: 0.001, OutputCostPer1K: 0.004},
	}
}

// TurnCost accumulates token usage and cost within a single agent turn.
type TurnCost struct {
	mu sync.Mutex

	ModelName      string  `json:"model_name"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	CacheHitTokens int64   `json:"cache_hit_tokens"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
}

// NewTurnCost creates a new turn cost accumulator.
func NewTurnCost(modelName string) *TurnCost {
	return &TurnCost{ModelName: modelName}
}

// AddInput records input token usage.
func (tc *TurnCost) AddInput(tokens int64) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.InputTokens += tokens
}

// AddOutput records output token usage.
func (tc *TurnCost) AddOutput(tokens int64) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.OutputTokens += tokens
}

// AddCacheHit records cache-hit tokens (billed at lower rate).
func (tc *TurnCost) AddCacheHit(tokens int64) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.CacheHitTokens += tokens
}

// TotalTokens returns the sum of all token types.
func (tc *TurnCost) TotalTokens() int64 {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.InputTokens + tc.OutputTokens + tc.CacheHitTokens
}

// ComputeCost calculates the USD cost using the pricing table.
func (tc *TurnCost) ComputeCost(pricing map[string]ModelPricing) float64 {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	p, ok := pricing[tc.ModelName]
	if !ok {
		return 0
	}

	// Cache hits are typically billed at 10% of input cost.
	cacheCost := float64(tc.CacheHitTokens) * p.InputCostPer1K * 0.1 / 1000.0
	inputCost := float64(tc.InputTokens) * p.InputCostPer1K / 1000.0
	outputCost := float64(tc.OutputTokens) * p.OutputCostPer1K / 1000.0

	tc.TotalCostUSD = cacheCost + inputCost + outputCost
	return tc.TotalCostUSD
}

// Snapshot returns a copy of the current cost state.
func (tc *TurnCost) Snapshot() TurnCost {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return TurnCost{
		ModelName:      tc.ModelName,
		InputTokens:    tc.InputTokens,
		OutputTokens:   tc.OutputTokens,
		CacheHitTokens: tc.CacheHitTokens,
		TotalCostUSD:   tc.TotalCostUSD,
	}
}

// ── Daily cost tracker ──────────────────────────────────────────────

// DailyCostTracker tracks cumulative costs per day for budget enforcement.
type DailyCostTracker struct {
	mu         sync.Mutex
	date       string // YYYY-MM-DD
	totalCost  float64
	totalCalls int64
}

// NewDailyCostTracker creates a new daily cost tracker.
func NewDailyCostTracker() *DailyCostTracker {
	return &DailyCostTracker{
		date: time.Now().UTC().Format("2006-01-02"),
	}
}

// Add records cost from a completed turn.
func (d *DailyCostTracker) Add(cost float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if today != d.date {
		d.date = today
		d.totalCost = 0
		d.totalCalls = 0
	}

	d.totalCost += cost
	d.totalCalls++
}

// ExceedsBudget returns true if the daily budget (in USD cents) would be
// exceeded by adding pendingCost.
func (d *DailyCostTracker) ExceedsBudget(maxCostCents int, pendingCost float64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if today != d.date {
		return false
	}

	maxCost := float64(maxCostCents) / 100.0
	return d.totalCost+pendingCost > maxCost
}

// TodayCost returns today's accumulated cost.
func (d *DailyCostTracker) TodayCost() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if today != d.date {
		return 0
	}
	return d.totalCost
}

// TodayCalls returns today's call count.
func (d *DailyCostTracker) TodayCalls() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.totalCalls
}
