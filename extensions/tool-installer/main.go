// Tool Installer extension for Mneme.
//
// Provides:
//   - install_tool: detect OS and install system packages via apt, brew, pip, npm, or go
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "tool-installer",
		Version:     "0.1.0",
		Description: "Detect OS and install system packages via apt, brew, pip, npm, or go",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "install_tool",
		Description: "Install a system package or tool. Detects OS and uses the appropriate package manager (apt, brew, pip, npm, go).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":   map[string]interface{}{"type": "string", "description": "Package/tool name to install"},
				"method": map[string]interface{}{"type": "string", "description": "Package manager: apt, brew, pip, npm, go, or auto-detect"},
			},
			"required": []string{"name"},
		},
		Permission: "execute",
		HasEffects: true,
	}, installTool)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tool-installer: %v\n", err)
		os.Exit(1)
	}
}

func installTool(ctx context.Context, args map[string]interface{}) extsdk.Result {
	name, _ := args["name"].(string)
	if name == "" {
		return extsdk.Result{Error: "name is required"}
	}
	if strings.HasPrefix(name, "-") {
		return extsdk.Result{Error: "invalid package name: names starting with '-' are not allowed"}
	}

	method, _ := args["method"].(string)
	if method == "" || method == "auto" {
		method = detectMethod(name)
	}

	var cmd *exec.Cmd
	switch strings.ToLower(method) {
	case "apt":
		cmd = exec.CommandContext(ctx, "sudo", "apt-get", "install", "-y", name)
	case "brew":
		cmd = exec.CommandContext(ctx, "brew", "install", name)
	case "pip":
		cmd = exec.CommandContext(ctx, "pip", "install", name)
	case "npm":
		cmd = exec.CommandContext(ctx, "npm", "install", "-g", name)
	case "go":
		cmd = exec.CommandContext(ctx, "go", "install", name+"@latest")
	default:
		return extsdk.Result{Error: fmt.Sprintf("unsupported method: %s (use: apt, brew, pip, npm, go)", method)}
	}

	// Check if the command exists
	if _, err := exec.LookPath(cmd.Path); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("command %q not found on this system", cmd.Path)}
	}

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		return extsdk.Result{Success: false, Output: outStr, Error: fmt.Sprintf("install failed: %v", err)}
	}

	result := map[string]interface{}{
		"os":      runtime.GOOS + "/" + runtime.GOARCH,
		"method":  method,
		"package": name,
		"status":  "installed",
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return extsdk.Result{Success: true, Output: string(b) + "\n\n" + outStr}
}

func detectMethod(name string) string {
	// Auto-detect based on OS and name patterns
	osName := runtime.GOOS

	if osName == "darwin" {
		// Check if brew exists
		if _, err := exec.LookPath("brew"); err == nil {
			return "brew"
		}
	}

	if name == "go" || strings.HasPrefix(name, "golang") {
		return "apt"
	}

	// Default to apt for Linux, brew for macOS
	if osName == "darwin" {
		return "brew"
	}
	return "apt"
}
