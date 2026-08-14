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
// allowed; methods will simply no-op.
func NewCostCallback(tracker *agent.DailyCostTracker) *CostCallback {
	return &CostCallback{
		tracker: tracker,
		pricing: agent.DefaultModelPricing(),
	}
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
