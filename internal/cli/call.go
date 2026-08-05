package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func runCall(args []string) error {
	toolName := ""
	var paramsJSON string

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
		default:
			if !strings.HasPrefix(args[i], "-") && toolName == "" {
				toolName = args[i]
			}
		}
	}

	if toolName == "" {
		fmt.Fprintln(os.Stderr, "Usage: mneme call --tool <name> [--params <json>]")
		fmt.Fprintln(os.Stderr, "       mneme call <tool-name> [json-params]")
		return fmt.Errorf("--tool is required")
	}

	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
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
