//go:build !linux
// +build !linux

package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func keyboardTool(_ context.Context, args map[string]interface{}) callToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		return callToolResult{Error: "action is required"}
	}

	switch runtime.GOOS {
	case "darwin":
		return keyboardMac(action, args)
	case "windows":
		return keyboardWindows(action, args)
	default:
		return callToolResult{Error: fmt.Sprintf("unsupported platform: %s", runtime.GOOS)}
	}
}

func keyboardMac(action string, args map[string]interface{}) callToolResult {
	switch action {
	case "type":
		text, _ := args["text"].(string)
		if text == "" {
			return callToolResult{Error: "text is required for type action"}
		}
		// osascript keystroke
		escaped := strings.ReplaceAll(text, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		cmd := exec.Command("osascript", "-e", fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("keyboard type: %v (%s)", err, string(out))}
		}
		return callToolResult{Success: true, Output: fmt.Sprintf("Typed: %s\n%s", text, string(out))}

	case "combo":
		keys := strSliceFromArgs(args, "keys")
		if len(keys) == 0 {
			return callToolResult{Error: "keys array required for combo action"}
		}
		// Map to AppleScript key codes
		macMods := map[string]string{
			"ctrl": "control", "control": "control",
			"alt": "option", "option": "option",
			"shift": "shift", "super": "command", "cmd": "command", "meta": "command",
		}
		mods := []string{}
		for _, k := range keys[:len(keys)-1] {
			if m, ok := macMods[strings.ToLower(k)]; ok {
				mods = append(mods, m+" down")
			}
		}
		lastKey := keys[len(keys)-1]
		script := fmt.Sprintf(`tell application "System Events"
    %s
    keystroke "%s"
    %s
end tell`,
			strings.Join(mods, "\n    "),
			lastKey,
			func() string {
				reversed := []string{}
				for _, m := range mods {
					reversed = append(reversed, strings.Replace(m, " down", " up", 1))
				}
				return strings.Join(reversed, "\n    ")
			}())
		cmd := exec.Command("osascript", "-e", script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("keyboard combo: %v (%s)", err, string(out))}
		}
		return callToolResult{Success: true, Output: string(out)}

	default:
		return callToolResult{Error: fmt.Sprintf("unknown keyboard action: %s", action)}
	}
}

func keyboardWindows(action string, args map[string]interface{}) callToolResult {
	switch action {
	case "type":
		text, _ := args["text"].(string)
		if text == "" {
			return callToolResult{Error: "text is required for type action"}
		}
		ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait('%s')`, escapePSSendKeys(text))
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("keyboard type: %v (%s)", err, string(out))}
		}
		return callToolResult{Success: true, Output: string(out)}

	case "combo":
		keys := strSliceFromArgs(args, "keys")
		if len(keys) == 0 {
			return callToolResult{Error: "keys array required for combo action"}
		}
		// PowerShell SendKeys notation: ^ for ctrl, % for alt, + for shift
		sendKeys := ""
		for _, k := range keys {
			switch strings.ToLower(k) {
			case "ctrl", "control":
				sendKeys += "^"
			case "alt":
				sendKeys += "%"
			case "shift":
				sendKeys += "+"
			default:
				sendKeys += "(" + k + ")"
			}
		}
		ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait('%s')`, sendKeys)
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("keyboard combo: %v (%s)", err, string(out))}
		}
		return callToolResult{Success: true, Output: string(out)}

	default:
		return callToolResult{Error: fmt.Sprintf("unknown keyboard action: %s", action)}
	}
}

func escapePSSendKeys(s string) string {
	s = strings.ReplaceAll(s, "+", "{+}")
	s = strings.ReplaceAll(s, "^", "{^}")
	s = strings.ReplaceAll(s, "%", "{%}")
	s = strings.ReplaceAll(s, "~", "{~}")
	s = strings.ReplaceAll(s, "(", "{(}")
	s = strings.ReplaceAll(s, ")", "{)}")
	s = strings.ReplaceAll(s, "[", "{[}")
	s = strings.ReplaceAll(s, "]", "{]}")
	s = strings.ReplaceAll(s, "{", "{{}")
	s = strings.ReplaceAll(s, "}", "{}}")
	return s
}
