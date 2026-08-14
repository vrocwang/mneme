package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/simon/mneme/pkg/extsdk"
)

// launchAppTool launches a desktop application.
func launchAppTool(ctx context.Context, args map[string]interface{}) extsdk.Result {
	name, _ := args["name"].(string)
	path, _ := args["path"].(string)
	appArgs := strSliceFromArgs(args, "args")
	wait, _ := args["wait"].(bool)

	if name == "" && path == "" {
		return extsdk.Result{Error: "either name or path is required"}
	}

	execPath := path
	if execPath == "" {
		execPath = resolveAppPath(name)
	}
	if execPath == "" {
		return extsdk.Result{Error: fmt.Sprintf("could not find application: %s", name)}
	}

	var cmd *exec.Cmd
	if len(appArgs) > 0 {
		cmd = exec.CommandContext(ctx, execPath, appArgs...)
	} else {
		cmd = exec.CommandContext(ctx, execPath)
	}

	if wait {
		out, err := cmd.CombinedOutput()
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("launch %s: %v (%s)", name, err, string(out))}
		}
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Launched %s (completed)\n%s", name, string(out))}
	}

	if err := cmd.Start(); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("launch %s: %v", name, err)}
	}
	// Don't wait — release the process
	go cmd.Wait()

	return extsdk.Result{Success: true, Output: fmt.Sprintf("Launched: %s (pid: %d)", name, cmd.Process.Pid)}
}

func resolveAppPath(name string) string {
	lower := strings.ToLower(name)

	switch runtime.GOOS {
	case "linux":
		// First check PATH
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
		// Try common names
		aliases := map[string]string{
			"firefox":            "firefox",
			"chrome":             "google-chrome",
			"chromium":           "chromium-browser",
			"code":               "code",
			"terminal":           "gnome-terminal",
			"files":              "nautilus",
			"settings":           "gnome-control-center",
			"calculator":         "gnome-calculator",
			"text editor":        "gedit",
			"system monitor":     "gnome-system-monitor",
			"vscode":             "code",
			"visual studio code": "code",
		}
		if alias, ok := aliases[lower]; ok {
			if path, err := exec.LookPath(alias); err == nil {
				return path
			}
		}
		return ""

	case "darwin":
		// On macOS, 'open -a' launches the named application.
		// Return "open" as the executable with "-a" and the name as args.
		return "open"

	case "windows":
		// Try common paths
		commonPaths := map[string]string{
			"notepad":    "notepad.exe",
			"calc":       "calc.exe",
			"explorer":   "explorer.exe",
			"cmd":        "cmd.exe",
			"powershell": "powershell.exe",
			"code":       fileInPath("Code.exe"),
			"chrome":     fileInPath("chrome.exe"),
			"firefox":    fileInPath("firefox.exe"),
		}
		if p, ok := commonPaths[lower]; ok && p != "" {
			return p
		}
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
		if path, err := exec.LookPath(name + ".exe"); err == nil {
			return path
		}
		return ""
	}

	return ""
}

func fileInPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}
