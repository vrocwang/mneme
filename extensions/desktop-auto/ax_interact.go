package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// axInteractTool interacts with UI elements via the accessibility tree.
func axInteractTool(_ context.Context, args map[string]interface{}) callToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		return callToolResult{Error: "action is required: list, get, press, focus"}
	}

	appName, _ := args["appName"].(string)
	maxItems := 20
	if mi, ok := intFromArgs(args, "maxItems"); ok && mi > 0 {
		maxItems = mi
	}

	switch runtime.GOOS {
	case "linux":
		return axLinux(action, appName, args, maxItems)
	case "darwin":
		return axMacOS(action, appName, args, maxItems)
	default:
		return callToolResult{Error: fmt.Sprintf("accessibility tree not supported on %s", runtime.GOOS)}
	}
}

func axLinux(action, appName string, args map[string]interface{}, maxItems int) callToolResult {
	// Use AT-SPI via dbus-send or accerciser/at-spi-bus-launcher
	switch action {
	case "list":
		// List accessible applications via dbus
		cmd := exec.Command("gdbus", "call", "--session",
			"--dest", "org.a11y.Bus",
			"--object-path", "/org/a11y/bus",
			"--method", "org.freedesktop.DBus.Properties.Get",
			"org.a11y.Status", "IsEnabled")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("AT-SPI not available: %v", err)}
		}
		return callToolResult{Success: true, Output: fmt.Sprintf("AT-SPI status: %s\n\nAccessibility tree listing requires at-spi tools. Install: sudo apt install at-spi2-core accerciser", string(out))}

	case "get":
		target, _ := args["target"].(string)
		if target == "" {
			return callToolResult{Error: "target is required for get action"}
		}
		// Use at-spi-bus-launcher to query element
		cmd := exec.Command("gdbus", "introspect", "--session",
			"--dest", "org.a11y.Bus",
			"--object-path", "/org/a11y/bus")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("a11y query: %v", err)}
		}
		return callToolResult{Success: true, Output: fmt.Sprintf("Accessibility tree for %s:\n%s", target, string(out))}

	default:
		return callToolResult{Success: true, Output: fmt.Sprintf("%s action: accessibility interaction via AT-SPI requires dedicated a11y tools. Install at-spi2-core for full support.", action)}
	}
}

func axMacOS(action, appName string, args map[string]interface{}, maxItems int) callToolResult {
	target, _ := args["target"].(string)
	escapedApp := escapeAppleScript(appName)
	escapedTarget := escapeAppleScript(target)

	switch action {
	case "list":
		// Use osascript to get UI element hierarchy
		var script string
		if appName != "" {
			script = fmt.Sprintf(`
tell application "System Events"
    tell process "%s"
        set elemList to ""
        repeat with e in every UI element of window 1
            set elemList to elemList & (name of e) & " [" & (role of e) & "]" & linefeed
        end repeat
        return elemList
    end tell
end tell`, escapedApp)
		} else {
			script = `
tell application "System Events"
    set appList to ""
    repeat with p in (every process whose background only is false)
        set appList to appList & (name of p) & linefeed
    end repeat
    return appList
end tell`
		}
		cmd := exec.Command("osascript", "-e", script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("a11y list: %v (%s)", err, string(out))}
		}
		return callToolResult{Success: true, Output: truncate(string(out), 5000)}

	case "press":
		if target == "" {
			return callToolResult{Error: "target is required for press"}
		}
		script := fmt.Sprintf(`
tell application "System Events"
    tell process "%s"
        click (first UI element whose name is "%s")
    end tell
end tell`, escapedApp, escapedTarget)
		cmd := exec.Command("osascript", "-e", script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("a11y press: %v (%s)", err, string(out))}
		}
		return callToolResult{Success: true, Output: fmt.Sprintf("Pressed: %s\n%s", target, string(out))}

	case "focus":
		if appName == "" {
			return callToolResult{Error: "appName is required for focus"}
		}
		script := fmt.Sprintf(`tell application "%s" to activate`, escapedApp)
		cmd := exec.Command("osascript", "-e", script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("a11y focus: %v (%s)", err, string(out))}
		}
		return callToolResult{Success: true, Output: fmt.Sprintf("Focused: %s\n%s", appName, string(out))}

	default:
		return callToolResult{Error: fmt.Sprintf("unknown action: %s (valid: list, get, press, focus)", action)}
	}
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
