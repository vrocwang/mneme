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

// mouseTool controls the mouse via platform-specific commands.
func mouseTool(_ context.Context, args map[string]interface{}) callToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		return callToolResult{Error: "action is required"}
	}

	x, _ := floatFromArgs(args, "x")
	y, _ := floatFromArgs(args, "y")

	switch runtime.GOOS {
	case "darwin":
		return mouseMac(action, x, y, args)
	case "windows":
		return mouseWindows(action, x, y, args)
	default:
		return callToolResult{Error: fmt.Sprintf("unsupported platform: %s", runtime.GOOS)}
	}
}

func mouseMac(action string, x, y float64, args map[string]interface{}) callToolResult {
	amount, _ := floatFromArgs(args, "amount")
	if amount == 0 {
		amount = 3
	}

	var script string
	switch action {
	case "move":
		// Use cliclick if available (brew install cliclick), otherwise try Python Quartz
		if _, err := exec.LookPath("cliclick"); err == nil {
			cmd := exec.Command("cliclick", fmt.Sprintf("m:%d,%d", int(x), int(y)))
			out, err := cmd.CombinedOutput()
			if err != nil {
				return callToolResult{Error: fmt.Sprintf("macOS mouse move: %v (%s)", err, string(out))}
			}
			return callToolResult{Success: true, Output: strings.TrimSpace(string(out))}
		}
		// Fall back to Python Quartz
		py := fmt.Sprintf(`import Quartz; Quartz.CGEventPost(Quartz.kCGHIDEventTap, Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventMouseMoved, (%d, %d), 0))`, int(x), int(y))
		cmd := exec.Command("python3", "-c", py)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("macOS mouse move: %v (%s) - install cliclick (brew install cliclick) or pyobjc", err, string(out))}
		}
		return callToolResult{Success: true, Output: strings.TrimSpace(string(out))}
	case "click":
		script = fmt.Sprintf(`
tell application "System Events"
    click at {%d, %d}
end tell`, int(x), int(y))
	case "dblclick":
		script = fmt.Sprintf(`
tell application "System Events"
    double click at {%d, %d}
end tell`, int(x), int(y))
	case "rightclick":
		script = fmt.Sprintf(`
tell application "System Events"
    tell process "Finder"
        click at {%d, %d} with button 2
    end tell
end tell`, int(x), int(y))
	case "scroll_up":
		script = fmt.Sprintf(`
tell application "System Events"
    repeat %d times
        key code 116
    end repeat
end tell`, int(amount))
	case "scroll_down":
		script = fmt.Sprintf(`
tell application "System Events"
    repeat %d times
        key code 121
    end repeat
end tell`, int(amount))
	default:
		return callToolResult{Error: fmt.Sprintf("unsupported action on macOS: %s", action)}
	}

	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("macOS mouse: %v (%s)", err, string(out))}
	}
	return callToolResult{Success: true, Output: strings.TrimSpace(string(out))}
}

func mouseWindows(action string, x, y float64, args map[string]interface{}) callToolResult {
	// On Windows, use PowerShell to control mouse via .NET
	amount, _ := floatFromArgs(args, "amount")
	if amount == 0 {
		amount = 3
	}

	var ps string
	switch action {
	case "move":
		ps = fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d)`, int(x), int(y))
	case "click":
		ps = fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d)
Add-Type -MemberDefinition '[DllImport("user32.dll")] public static extern void mouse_event(int dwFlags, int dx, int dy, int cButtons, int dwExtraInfo);' -Name Win32 -Namespace System
[System.Win32]::mouse_event(0x0002, 0, 0, 0, 0)
[System.Win32]::mouse_event(0x0004, 0, 0, 0, 0)`, int(x), int(y))
	case "scroll_up":
		ps = fmt.Sprintf(`Add-Type -MemberDefinition '[DllImport("user32.dll")] public static extern void mouse_event(int dwFlags, int dx, int dy, int cButtons, int dwExtraInfo);' -Name Win32 -Namespace System
for ($i=0; $i -lt %d; $i++) { [System.Win32]::mouse_event(0x0800, 0, 0, 120, 0) }`, int(amount))
	case "scroll_down":
		ps = fmt.Sprintf(`Add-Type -MemberDefinition '[DllImport("user32.dll")] public static extern void mouse_event(int dwFlags, int dx, int dy, int cButtons, int dwExtraInfo);' -Name Win32 -Namespace System
for ($i=0; $i -lt %d; $i++) { [System.Win32]::mouse_event(0x0800, 0, 0, -120, 0) }`, int(amount))
	default:
		return callToolResult{Error: fmt.Sprintf("unsupported action on Windows: %s", action)}
	}

	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("Windows mouse: %v (%s)", err, string(out))}
	}
	return callToolResult{Success: true, Output: strings.TrimSpace(string(out))}
}

// Shared helpers used on non-Linux platforms.

func floatFromArgs(args map[string]interface{}, key string) (float64, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func intFromArgs(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

func strSliceFromArgs(args map[string]interface{}, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
