package approval

import (
	"sort"
)

// RPC provides Wails-bound approval gate methods.
type ApprovalRPC struct {
	gate *Gate
}

// NewApprovalRPC creates an approval RPC handler.
// The gate is typically nil at bind time and set later via SetGate
// once the approval gate is created during startup.
func NewApprovalRPC(gate *Gate) *ApprovalRPC {
	return &ApprovalRPC{gate: gate}
}

// SetGate updates the approval gate reference at runtime.
func (r *ApprovalRPC) SetGate(gate *Gate) {
	r.gate = gate
}

// ListPendingApprovals returns all pending approval requests.
func (r *ApprovalRPC) ListPendingApprovals() []map[string]interface{} {
	if r.gate == nil {
		return []map[string]interface{}{}
	}
	pending := r.gate.ListPending()
	result := make([]map[string]interface{}, len(pending))
	for i, p := range pending {
		result[i] = map[string]interface{}{
			"id":         p.ID,
			"tool_name":  p.ToolName,
			"args":       p.Args,
			"reason":     p.Reason,
			"created_at": p.CreatedAt,
			"expires_at": p.ExpiresAt,
		}
	}
	return result
}

// DecideApproval resolves a pending approval by ID.
// decision is one of "approve_once", "approve_always", or "deny".
func (r *ApprovalRPC) DecideApproval(id string, decision string) error {
	if r.gate == nil {
		return nil
	}
	var d Decision
	switch decision {
	case "approve_once":
		d = DecisionApproveOnce
	case "approve_always":
		d = DecisionApproveAlways
	default:
		d = DecisionDeny
	}
	return r.gate.Decide(id, d)
}

// ListApprovalAllowlist returns the permanent tool allowlist.
func (r *ApprovalRPC) ListApprovalAllowlist() []map[string]interface{} {
	if r.gate == nil {
		return []map[string]interface{}{}
	}
	entries, err := r.gate.ListAllowlist()
	if err != nil {
		return []map[string]interface{}{}
	}
	result := make([]map[string]interface{}, len(entries))
	for i, e := range entries {
		result[i] = map[string]interface{}{
			"tool_name": e.ToolName,
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i]["tool_name"].(string) < result[j]["tool_name"].(string)
	})
	return result
}
