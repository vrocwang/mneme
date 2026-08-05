//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"strings"
)

// keyboardTool controls the keyboard on Linux via xdotool.
func keyboardTool(_ context.Context, args map[string]interface{}) callToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		return callToolResult{Error: "action is required"}
	}

	if !hasXdotool() {
		return callToolResult{Error: "xdotool not found. Install with: sudo apt install xdotool"}
	}

	switch action {
	case "type":
		text, _ := args["text"].(string)
		if text == "" {
			return callToolResult{Error: "text is required for type action"}
		}
		out, err := execCmd("xdotool", "type", text)
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("keyboard type: %v", err)}
		}
		return callToolResult{Success: true, Output: fmt.Sprintf("Typed text (%d chars)\n%s", len(text), out)}

	case "combo":
		keys := strSliceFromArgs(args, "keys")
		if len(keys) == 0 {
			return callToolResult{Error: "keys array required for combo action (e.g. [\"ctrl\", \"c\"])"}
		}
		// xdotool uses key names like "ctrl+c", "alt+Tab"
		combo := strings.Join(keys, "+")
		combo = strings.ReplaceAll(combo, "ctrl", "ctrl")
		combo = strings.ReplaceAll(combo, "alt", "alt")
		combo = strings.ReplaceAll(combo, "shift", "shift")
		combo = strings.ReplaceAll(combo, "super", "super")
		combo = strings.ReplaceAll(combo, "meta", "super")

		out, err := execCmd("xdotool", "key", combo)
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("keyboard combo: %v", err)}
		}
		return callToolResult{Success: true, Output: fmt.Sprintf("Pressed: %s\n%s", combo, out)}

	case "enter":
		out, err := execCmd("xdotool", "key", "Return")
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("keyboard enter: %v", err)}
		}
		return callToolResult{Success: true, Output: out}

	default:
		return callToolResult{Error: fmt.Sprintf("unknown keyboard action: %s (valid: type, combo, enter)", action)}
	}
}
