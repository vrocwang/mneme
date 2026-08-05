package approval

import (
	"encoding/json"

	"github.com/simon/mneme/pkg/rpc"
)

// AllControllerSchemas returns the schema metadata for every approval controller.
func AllControllerSchemas() []rpc.ControllerSchema {
	return []rpc.ControllerSchema{
		{
			Namespace:   "approval",
			Method:      "list_pending",
			Description: "List all pending approval requests waiting for user action",
			Output: []rpc.FieldSchema{
				{Name: "requests", Type: rpc.TypeArray, Description: "Pending approval requests"},
			},
		},
		{
			Namespace:   "approval",
			Method:      "decide",
			Description: "Resolve a pending approval with approve_once, approve_always, or deny",
			Input: []rpc.FieldSchema{
				{Name: "id", Type: rpc.TypeString, Description: "Approval request ID", Required: true},
				{Name: "decision", Type: rpc.TypeString, Description: "approve_once, approve_always, or deny", Required: true},
			},
		},
		{
			Namespace:   "approval",
			Method:      "list_recent_decisions",
			Description: "List recently decided approval requests",
			Input: []rpc.FieldSchema{
				{Name: "limit", Type: rpc.TypeNumber, Description: "Max entries to return (default 50)"},
			},
		},
		{
			Namespace:   "approval",
			Method:      "list_allowlist",
			Description: "List permanently allowed tools",
		},
		{
			Namespace:   "approval",
			Method:      "remove_allowlist_entry",
			Description: "Remove a tool from the permanent allowlist",
			Input: []rpc.FieldSchema{
				{Name: "tool_name", Type: rpc.TypeString, Description: "Tool name to remove", Required: true},
			},
		},
	}
}

// AllRegisteredControllers returns the handler-backed controllers for the approval domain.
func AllRegisteredControllers(gate *Gate) []rpc.RegisteredController {
	return []rpc.RegisteredController{
		{
			Schema: rpc.ControllerSchema{
				Namespace:   "approval",
				Method:      "list_pending",
				Description: "List all pending approval requests",
			},
			Handler: func(args json.RawMessage) rpc.RpcOutcome {
				pending := gate.ListPending()
				return rpc.NewOutcome(map[string]interface{}{"requests": pending})
			},
		},
		{
			Schema: rpc.ControllerSchema{
				Namespace:   "approval",
				Method:      "decide",
				Description: "Resolve a pending approval",
			},
			Handler: func(args json.RawMessage) rpc.RpcOutcome {
				var req struct {
					ID       string `json:"id"`
					Decision string `json:"decision"`
				}
				if err := json.Unmarshal(args, &req); err != nil {
					return rpc.NewErrorOutcome(rpc.ErrCodeInvalidArgs, "invalid arguments", err.Error())
				}

				var decision Decision
				switch req.Decision {
				case "approve_once":
					decision = DecisionApproveOnce
				case "approve_always":
					decision = DecisionApproveAlways
				case "deny":
					decision = DecisionDeny
				default:
					return rpc.NewErrorOutcome(rpc.ErrCodeInvalidArgs, "invalid decision", req.Decision)
				}

				if err := gate.Decide(req.ID, decision); err != nil {
					return rpc.NewErrorOutcome(rpc.ErrCodeNotFound, "decide failed", err.Error())
				}
				return rpc.NewOutcome(map[string]interface{}{"ok": true})
			},
		},
		{
			Schema: rpc.ControllerSchema{
				Namespace:   "approval",
				Method:      "list_recent_decisions",
				Description: "List recently decided approvals",
			},
			Handler: func(args json.RawMessage) rpc.RpcOutcome {
				var req struct {
					Limit int `json:"limit"`
				}
				if len(args) > 0 {
					json.Unmarshal(args, &req)
				}
				if req.Limit <= 0 {
					req.Limit = 50
				}
				entries, err := gate.ListRecentDecisions(req.Limit)
				if err != nil {
					return rpc.NewErrorOutcome(rpc.ErrCodeInternal, "list failed", err.Error())
				}
				return rpc.NewOutcome(map[string]interface{}{"entries": entries})
			},
		},
		{
			Schema: rpc.ControllerSchema{
				Namespace:   "approval",
				Method:      "list_allowlist",
				Description: "List permanently allowed tools",
			},
			Handler: func(args json.RawMessage) rpc.RpcOutcome {
				entries, err := gate.ListAllowlist()
				if err != nil {
					return rpc.NewErrorOutcome(rpc.ErrCodeInternal, "list failed", err.Error())
				}
				return rpc.NewOutcome(map[string]interface{}{"entries": entries})
			},
		},
		{
			Schema: rpc.ControllerSchema{
				Namespace:   "approval",
				Method:      "remove_allowlist_entry",
				Description: "Remove a tool from the permanent allowlist",
			},
			Handler: func(args json.RawMessage) rpc.RpcOutcome {
				var req struct {
					ToolName string `json:"tool_name"`
				}
				if err := json.Unmarshal(args, &req); err != nil {
					return rpc.NewErrorOutcome(rpc.ErrCodeInvalidArgs, "invalid arguments", err.Error())
				}
				if err := gate.RemoveAllowlistEntry(req.ToolName); err != nil {
					return rpc.NewErrorOutcome(rpc.ErrCodeInternal, "remove failed", err.Error())
				}
				return rpc.NewOutcome(map[string]interface{}{"ok": true})
			},
		},
	}
}
