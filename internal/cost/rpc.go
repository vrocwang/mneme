package cost

// RPC provides Wails-bound cost tracker methods.
//
// The tracker is resolved through a getter rather than captured at construction
// time: Wails evaluates the Bind list before OnStartup runs, so a captured
// pointer would be nil for the whole process lifetime.
type CostRPC struct {
	trackerFn func() *Tracker
}

// NewCostRPC creates a cost RPC handler. trackerFn is evaluated per call.
func NewCostRPC(trackerFn func() *Tracker) *CostRPC {
	return &CostRPC{trackerFn: trackerFn}
}

// tracker resolves the cost tracker lazily.
func (r *CostRPC) tracker() *Tracker {
	if r.trackerFn == nil {
		return nil
	}
	return r.trackerFn()
}

// GetCostOverview returns a human-readable cost summary.
func (r *CostRPC) GetCostOverview() string {
	t := r.tracker()
	if t == nil {
		return "Cost tracking not available."
	}
	return t.Overview()
}

// GetCostDashboard returns structured cost data for the settings UI.
func (r *CostRPC) GetCostDashboard() map[string]interface{} {
	t := r.tracker()
	if t == nil {
		return map[string]interface{}{"ok": false}
	}
	return map[string]interface{}{
		"ok":               true,
		"overview":         t.Overview(),
		"total_cost_cents": t.TotalCost(),
		"budget_used_pct":  t.BudgetUsed(),
	}
}
