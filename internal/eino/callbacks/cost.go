package callbacks

import (
	"github.com/simon/mneme/internal/agent"
)

// CostCallback wraps a DailyCostTracker to record token usage and
// compute costs from the eino pipeline. All methods are nil-safe.
type CostCallback struct {
	tracker *agent.DailyCostTracker
	pricing map[string]agent.ModelPricing
}

// NewCostCallback creates a CostCallback. Passing nil for tracker is
// allowed; methods will simply no-op. Pricing defaults to the built-in
// table but can be overridden via SetPricing.
func NewCostCallback(tracker *agent.DailyCostTracker) *CostCallback {
	return &CostCallback{
		tracker: tracker,
		pricing: agent.DefaultModelPricing(),
	}
}

// SetPricing replaces the pricing table used for cost calculation.
func (c *CostCallback) SetPricing(pricing map[string]agent.ModelPricing) {
	c.pricing = pricing
}

// OnTokens records token usage for a model invocation and adds the
// computed cost to the daily tracker. inputTokens, outputTokens, and
// cacheTokens are the raw token counts from the provider response.
func (c *CostCallback) OnTokens(model string, inputTokens, outputTokens, cacheTokens int) {
	if c.tracker == nil {
		return
	}

	tc := agent.NewTurnCost(model)
	tc.AddInput(int64(inputTokens))
	tc.AddOutput(int64(outputTokens))
	if cacheTokens > 0 {
		tc.AddCacheHit(int64(cacheTokens))
	}
	cost := tc.ComputeCost(c.pricing)
	c.tracker.Add(cost)
}

// TodayCost returns today's accumulated cost. Returns 0 when the
// tracker is nil.
func (c *CostCallback) TodayCost() float64 {
	if c.tracker == nil {
		return 0
	}
	return c.tracker.TodayCost()
}

// TodayCalls returns today's call count. Returns 0 when the tracker
// is nil.
func (c *CostCallback) TodayCalls() int64 {
	if c.tracker == nil {
		return 0
	}
	return c.tracker.TodayCalls()
}
