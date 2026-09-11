package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/simon/mneme/internal/tools"
)

func runCall(args []string) error {
	toolName := ""
	var paramsJSON string
	assumeYes := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tool", "-t":
			if i+1 < len(args) {
				toolName = args[i+1]
				i++
			}
		case "--params", "-p":
			if i+1 < len(args) {
				paramsJSON = args[i+1]
				i++
			}
		case "--yes", "-y":
			assumeYes = true
		default:
			if !strings.HasPrefix(args[i], "-") && toolName == "" {
				toolName = args[i]
			}
		}
	}

	if toolName == "" {
		fmt.Fprintln(os.Stderr, "Usage: mneme call --tool <name> [--params <json>] [--yes]")
		fmt.Fprintln(os.Stderr, "       mneme call <tool-name> [json-params] [--yes]")
		fmt.Fprintln(os.Stderr, "       --yes is required for tools that write, execute, or have side effects")
		return fmt.Errorf("--tool is required")
	}

	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	// The CLI is a direct execution surface and does not run the agent's
	// security middleware, so gate risky tools behind an explicit opt-in
	// rather than silently bypassing the approval model.
	if t, ok := core.CapReg.GetTool(toolName); ok && toolRequiresConfirmation(t) && !assumeYes {
		return fmt.Errorf("tool %q can write, execute, or has side effects; re-run with --yes to confirm", toolName)
	}

	var params map[string]interface{}
	if paramsJSON != "" {
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return fmt.Errorf("invalid params JSON: %w", err)
		}
	}
	if params == nil {
		params = map[string]interface{}{}
	}

	result := core.CapReg.Execute(context.Background(), toolName, params)
	if result.Error != "" {
		fmt.Fprintln(os.Stderr, "Error:", result.Error)
		os.Exit(1)
	}
	fmt.Println(result.Output)
	return nil
}

// toolRequiresConfirmation reports whether a tool can modify state or execute
// code, and therefore needs an explicit --yes before CLI execution.
func toolRequiresConfirmation(t tools.Tool) bool {
	if pt, ok := t.(tools.PermissionedTool); ok {
		switch pt.PermissionLevel() {
		case tools.PermWrite, tools.PermExecute, tools.PermDangerous:
			return true
		}
	}
	if et, ok := t.(tools.SideEffectTool); ok && et.SideEffects() {
		return true
	}
	return false
}
