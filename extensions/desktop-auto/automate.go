package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type automationAction struct {
	Action string                 `json:"action"` // mouse, keyboard, sleep
	Args   map[string]interface{} `json:"args,omitempty"`
	Desc   string                 `json:"desc,omitempty"` // human-readable description
}

// automateTool runs a sequence of mouse and keyboard actions from a JSON plan.
func automateTool(ctx context.Context, args map[string]interface{}) callToolResult {
	planStr, _ := args["plan"].(string)
	if planStr == "" {
		return callToolResult{Error: "plan is required (JSON array of action objects)"}
	}

	var actions []automationAction
	if err := json.Unmarshal([]byte(planStr), &actions); err != nil {
		return callToolResult{Error: fmt.Sprintf("invalid plan JSON: %v", err)}
	}

	if len(actions) == 0 {
		return callToolResult{Error: "plan must contain at least one action"}
	}

	delayMs := 100
	if d, ok := floatFromArgs(args, "delayMs"); ok && d > 0 {
		delayMs = int(d)
	}

	var results []string
	var errors []string

	for i, a := range actions {
		select {
		case <-ctx.Done():
			errors = append(errors, fmt.Sprintf("cancelled after %d actions", i))
			return buildAutomateResult(results, errors, i+1, len(actions))
		default:
		}

		time.Sleep(time.Duration(delayMs) * time.Millisecond)

		var result callToolResult
		switch a.Action {
		case "mouse":
			result = mouseTool(ctx, a.Args)
		case "keyboard":
			result = keyboardTool(ctx, a.Args)
		case "sleep":
			ms, _ := floatFromArgs(a.Args, "ms")
			if ms > 0 {
				time.Sleep(time.Duration(ms) * time.Millisecond)
				results = append(results, fmt.Sprintf("  [%d] sleep %dms - OK", i+1, int(ms)))
			}
			continue
		default:
			errors = append(errors, fmt.Sprintf("action %d: unknown action %q", i+1, a.Action))
			continue
		}

		label := a.Desc
		if label == "" {
			label = fmt.Sprintf("%s[%d]", a.Action, i+1)
		}

		if result.Error != "" {
			errors = append(errors, fmt.Sprintf("action %d (%s): %s", i+1, label, result.Error))
		} else {
			results = append(results, fmt.Sprintf("  [%d] %s - OK: %s", i+1, label, truncate(result.Output, 80)))
		}
	}

	return buildAutomateResult(results, errors, len(actions), len(actions))
}

func buildAutomateResult(results []string, errors []string, executed, total int) callToolResult {
	var out strings.Builder
	out.WriteString(fmt.Sprintf("Automation: %d/%d actions executed\n", executed, total))

	if len(results) > 0 {
		out.WriteString("\nResults:\n")
		out.WriteString(strings.Join(results, "\n"))
	}
	if len(errors) > 0 {
		out.WriteString("\n\nErrors:\n")
		out.WriteString(strings.Join(errors, "\n"))
	}

	if len(errors) > 0 && len(results) == 0 {
		return callToolResult{Error: out.String()}
	}

	return callToolResult{Success: true, Output: out.String()}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
