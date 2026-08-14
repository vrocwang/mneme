// Desktop Auto extension for Mneme.
//
// Provides desktop automation tools:
//   - mouse: move, click, drag, scroll
//   - keyboard: type text, press key combinations
//   - automate: run sequences of mouse/keyboard actions
//   - ax_interact: interact with UI elements via accessibility tree
//   - launch_app: launch desktop applications
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"fmt"
	"os"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "desktop-auto",
		Version:     "0.1.0",
		Description: "Desktop automation: mouse, keyboard, automate, ax_interact, launch_app",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "mouse",
		Description: "Control the mouse: move to coordinates, click (left/right/middle), double-click, drag, scroll",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "description": "Action: move, click, dblclick, rightclick, drag, scroll_up, scroll_down"},
				"x":      map[string]interface{}{"type": "number", "description": "X coordinate (for move/click/drag)"},
				"y":      map[string]interface{}{"type": "number", "description": "Y coordinate (for move/click/drag)"},
				"dx":     map[string]interface{}{"type": "number", "description": "Delta X (for drag)"},
				"dy":     map[string]interface{}{"type": "number", "description": "Delta Y (for drag)"},
				"amount": map[string]interface{}{"type": "number", "description": "Scroll amount (for scroll)"},
			},
			"required": []string{"action"},
		},
		Permission: "execute",
		HasEffects: true,
	}, mouseTool)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "keyboard",
		Description: "Control the keyboard: type text, press key combinations (ctrl+c, alt+tab, etc.)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "description": "Action: type, combo"},
				"text":   map[string]interface{}{"type": "string", "description": "Text to type (for type action)"},
				"keys":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Keys for combo: [\"ctrl\", \"c\"] or [\"alt\", \"tab\"]"},
			},
			"required": []string{"action"},
		},
		Permission: "execute",
		HasEffects: true,
	}, keyboardTool)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "automate",
		Description: "Run a sequence of mouse and keyboard actions from a JSON plan",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"plan":    map[string]interface{}{"type": "string", "description": "JSON array of action objects, each with action and parameters"},
				"delayMs": map[string]interface{}{"type": "number", "description": "Delay between actions in milliseconds (default 100)"},
			},
			"required": []string{"plan"},
		},
		Permission: "execute",
		HasEffects: true,
	}, automateTool)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "ax_interact",
		Description: "Interact with UI elements via the accessibility tree: list elements, get properties, perform actions (press, increment, decrement)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "description": "Action: list, get, press, focus"},
				"target":   map[string]interface{}{"type": "string", "description": "Element description or accessibility identifier"},
				"appName":  map[string]interface{}{"type": "string", "description": "Application name to scope the search"},
				"maxItems": map[string]interface{}{"type": "number", "description": "Max items to return (default 20)"},
			},
			"required": []string{"action"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, axInteractTool)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "launch_app",
		Description: "Launch a desktop application by name or path",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Application name (e.g. 'firefox', 'code', 'terminal')"},
				"path": map[string]interface{}{"type": "string", "description": "Full path to executable (alternative to name)"},
				"args": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Command line arguments"},
				"wait": map[string]interface{}{"type": "boolean", "description": "Wait for process to finish (default false)"},
			},
			"required": []string{},
		},
		Permission: "execute",
		HasEffects: true,
	}, launchAppTool)

	srv.RegisterAgent(extsdk.AgentDef{
		ID:          "desktop_control_agent",
		Name:        "Desktop Controller",
		Description: "Automates desktop interactions: mouse, keyboard, accessibility tree navigation, and app launching",
		Tier:        "worker",
		SystemPrompt: `You are a desktop automation specialist. Use mouse, keyboard, and accessibility tools to control the desktop.
- Verify element visibility before interacting
- Add pauses between actions to allow UI to respond
- Handle errors gracefully — report what failed and why`,
		ToolAllowlist: []string{"mouse", "keyboard", "automate", "ax_interact", "launch_app", "read_file", "shell"},
		MaxIterations: 12,
		Hidden:        false,
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "desktop-auto: %v\n", err)
		os.Exit(1)
	}
}
