package desktop

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/simon/mneme/internal/tools"
)

// DesktopAutomateTool wraps the Automator so agents can run desktop
// automation sequences (click, type, find, vision_click, etc.).
type DesktopAutomateTool struct {
	automator *Automator
}

// NewDesktopAutomateTool creates a desktop automation tool.
func NewDesktopAutomateTool(automator *Automator) *DesktopAutomateTool {
	return &DesktopAutomateTool{automator: automator}
}

func (t *DesktopAutomateTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "desktop_automate",
		Description: "Run desktop automation: click (x,y), type (text), key_combo (keys), wait (ms), find (accessibility search), vision_click (screen-vision locate+click).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"steps": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"action":  map[string]interface{}{"type": "string", "description": "click, right_click, double_click, type, key, key_combo, wait, find, vision_click"},
							"x":       map[string]interface{}{"type": "integer"},
							"y":       map[string]interface{}{"type": "integer"},
							"text":    map[string]interface{}{"type": "string", "description": "Text to type"},
							"keys":    map[string]interface{}{"type": "string", "description": "Keys to press (e.g., ctrl+c, enter)"},
							"query":   map[string]interface{}{"type": "string", "description": "Search term for find or vision_click"},
							"wait_ms": map[string]interface{}{"type": "integer", "description": "Milliseconds to wait"},
						},
					},
				},
			},
			"required": []string{"steps"},
		},
	}
}

func (t *DesktopAutomateTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	if t.automator == nil {
		return tools.Result{Error: "desktop automator not available"}
	}
	stepsRaw, ok := args["steps"].([]interface{})
	if !ok || len(stepsRaw) == 0 {
		return tools.Result{Error: "steps array is required"}
	}
	steps := make([]AutomationStep, 0, len(stepsRaw))
	for i, raw := range stepsRaw {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return tools.Result{Error: fmt.Sprintf("step %d is not an object, got %T", i, raw)}
		}
		steps = append(steps, AutomationStep{
			Action: strVal(m, "action"), X: intVal(m, "x"), Y: intVal(m, "y"),
			Text: strVal(m, "text"), Keys: strVal(m, "keys"),
			Query: strVal(m, "query"), WaitMs: intVal(m, "wait_ms"),
		})
	}
	if len(steps) == 0 {
		return tools.Result{Error: "no valid steps"}
	}
	result := t.automator.Run(ctx, steps)
	b, err := json.Marshal(result)
	if err != nil {
		return tools.Result{Error: fmt.Errorf("marshal automate result: %w", err).Error()}
	}
	return tools.Result{Success: result.Success, Output: string(b)}
}

func (t *DesktopAutomateTool) PermissionLevel() tools.PermissionLevel { return tools.PermExecute }
func (t *DesktopAutomateTool) SideEffects() bool                      { return true }

func strVal(m map[string]interface{}, k string) string {
	s, _ := m[k].(string)
	return s
}

func intVal(m map[string]interface{}, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}
