package cost

// RPC provides Wails-bound cost tracker methods.
type CostRPC struct {
	tracker *Tracker
}

// NewRPC creates a cost RPC handler.
func NewCostRPC(tracker *Tracker) *CostRPC {
	return &CostRPC{tracker: tracker}
}

// GetCostOverview returns a human-readable cost summary.
func (r *CostRPC) GetCostOverview() string {
	if r.tracker == nil {
		return "Cost tracking not available."
	}
	return r.tracker.Overview()
}

// GetCostDashboard returns structured cost data for the settings UI.
func (r *CostRPC) GetCostDashboard() map[string]interface{} {
	if r.tracker == nil {
		return map[string]interface{}{"ok": false}
	}
	return map[string]interface{}{
		"ok":               true,
		"overview":         r.tracker.Overview(),
		"total_cost_cents": r.tracker.TotalCost(),
		"budget_used_pct":  r.tracker.BudgetUsed(),
	}
}
