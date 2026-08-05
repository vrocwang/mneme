package memory

import (
	"encoding/json"

	"github.com/simon/mneme/pkg/rpc"
)

// RegisterToolRuleControllers registers all tool rule handlers with a live store.
func RegisterToolRuleControllers(registry *rpc.ControllerRegistry, store *ToolRuleStore) {
	registry.Register(rpc.RegisteredController{
		Schema: rpc.ControllerSchema{
			Namespace:   "tool_rule",
			Method:      "put",
			Description: "Create or update a tool-specific memory rule",
			Input: []rpc.FieldSchema{
				{Name: "tool_name", Type: rpc.TypeString, Required: true, Description: "The tool this rule applies to"},
				{Name: "content", Type: rpc.TypeString, Required: true, Description: "The rule content"},
				{Name: "priority", Type: rpc.TypeString, Required: false, Description: "critical, high, normal, or low"},
				{Name: "source", Type: rpc.TypeString, Required: false, Description: "manual, heuristic, or programmatic"},
			},
		},
		Handler: func(args json.RawMessage) rpc.RpcOutcome {
			var req struct {
				ToolName string `json:"tool_name"`
				Content  string `json:"content"`
				Priority string `json:"priority"`
				Source   string `json:"source"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return rpc.NewErrorOutcome(rpc.ErrCodeInvalidArgs, "invalid arguments", err.Error())
			}
			if req.ToolName == "" || req.Content == "" {
				return rpc.NewErrorOutcome(rpc.ErrCodeInvalidArgs, "tool_name and content are required", "")
			}
			rule := ToolRule{
				ToolName: req.ToolName,
				Content:  req.Content,
				Priority: RulePriority(req.Priority),
				Source:   RuleSource(req.Source),
			}
			if err := store.Put(rule); err != nil {
				return rpc.NewErrorOutcome(rpc.ErrCodeInternal, "failed to save rule", err.Error())
			}
			return rpc.NewOutcome(rule)
		},
	})

	registry.Register(rpc.RegisteredController{
		Schema: rpc.ControllerSchema{
			Namespace:   "tool_rule",
			Method:      "list",
			Description: "List all rules for a tool",
			Input: []rpc.FieldSchema{
				{Name: "tool_name", Type: rpc.TypeString, Required: true, Description: "The tool to list rules for"},
			},
		},
		Handler: func(args json.RawMessage) rpc.RpcOutcome {
			var req struct {
				ToolName string `json:"tool_name"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return rpc.NewErrorOutcome(rpc.ErrCodeInvalidArgs, "invalid arguments", err.Error())
			}
			rules, err := store.List(req.ToolName)
			if err != nil {
				return rpc.NewErrorOutcome(rpc.ErrCodeInternal, "failed to list rules", err.Error())
			}
			if rules == nil {
				rules = []ToolRule{}
			}
			return rpc.NewOutcome(rules)
		},
	})

	registry.Register(rpc.RegisteredController{
		Schema: rpc.ControllerSchema{
			Namespace:   "tool_rule",
			Method:      "delete",
			Description: "Delete a tool rule by ID",
			Input: []rpc.FieldSchema{
				{Name: "id", Type: rpc.TypeString, Required: true, Description: "The rule ID to delete"},
			},
		},
		Handler: func(args json.RawMessage) rpc.RpcOutcome {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return rpc.NewErrorOutcome(rpc.ErrCodeInvalidArgs, "invalid arguments", err.Error())
			}
			if err := store.Delete(req.ID); err != nil {
				return rpc.NewErrorOutcome(rpc.ErrCodeInternal, "failed to delete rule", err.Error())
			}
			return rpc.NewOutcome(map[string]string{"status": "ok"})
		},
	})

	registry.Register(rpc.RegisteredController{
		Schema: rpc.ControllerSchema{
			Namespace:   "tool_rule",
			Method:      "for_prompt",
			Description: "Get critical and high priority rules for prompt injection",
			Input: []rpc.FieldSchema{
				{Name: "tool_name", Type: rpc.TypeString, Required: false, Description: "Optional: filter by tool name"},
			},
		},
		Handler: func(args json.RawMessage) rpc.RpcOutcome {
			var req struct {
				ToolName string `json:"tool_name"`
			}
			json.Unmarshal(args, &req)
			var rules []ToolRule
			var err error
			if req.ToolName != "" {
				rules, err = store.ForPrompt(req.ToolName)
			} else {
				rules, err = store.ForPromptAll()
			}
			if err != nil {
				return rpc.NewErrorOutcome(rpc.ErrCodeInternal, "failed to get rules", err.Error())
			}
			if rules == nil {
				rules = []ToolRule{}
			}
			return rpc.NewOutcome(map[string]interface{}{
				"rules":       rules,
				"prompt_text": store.BuildPromptSection(),
			})
		},
	})
}
