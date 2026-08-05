package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── run_tests ─────────────────────────────────────────────────────────

func NewRunTests(workspaceRoot string) Tool {
	return &runTestsTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "run_tests",
				Description: "Runs the test suite for the workspace. Supports Go (go test), Rust (cargo test), JS/TS (vitest/jest), and Python (pytest).",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"framework": map[string]interface{}{
							"type":        "string",
							"description": "Test framework: 'go', 'cargo', 'vitest', 'jest', 'pytest', or 'auto' (default: auto-detect).",
						},
						"filter": map[string]interface{}{
							"type":        "string",
							"description": "Optional test name/pattern filter.",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Optional subdirectory or specific test file.",
						},
					},
				},
			},
			PermLevel:      PermExecute,
			HasSideEffects: true,
			MaxOutputChars: 20000,
			ToolCategory:   CategorySystem,
		},
		workspaceRoot: workspaceRoot,
	}
}

type runTestsTool struct {
	BaseTool
	workspaceRoot string
}

func (t *runTestsTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	framework, _ := args["framework"].(string)
	filter, _ := args["filter"].(string)
	testPath, _ := args["path"].(string)

	if framework == "" || framework == "auto" {
		framework = detectTestFramework(t.workspaceRoot)
	}
	if framework == "" {
		return Result{Success: false, Error: "No test framework detected. Available: go, cargo, vitest, jest, pytest."}
	}

	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	var cmdArgs []string
	switch framework {
	case "go":
		cmdArgs = []string{"go", "test", "-count=1"}
		if testPath != "" {
			cmdArgs = append(cmdArgs, "./"+testPath+"/...")
		} else {
			cmdArgs = append(cmdArgs, "./...")
		}
		if filter != "" {
			cmdArgs = append(cmdArgs, "-run", filter)
		}
	case "cargo":
		cmdArgs = []string{"cargo", "test"}
		if filter != "" {
			cmdArgs = append(cmdArgs, filter)
		}
	case "vitest":
		cmdArgs = []string{"npx", "vitest", "run"}
		if filter != "" {
			cmdArgs = append(cmdArgs, "-t", filter)
		}
		if testPath != "" {
			cmdArgs = append(cmdArgs, testPath)
		}
	case "jest":
		cmdArgs = []string{"npx", "jest"}
		if filter != "" {
			cmdArgs = append(cmdArgs, "-t", filter)
		}
		if testPath != "" {
			cmdArgs = append(cmdArgs, testPath)
		}
	case "pytest":
		cmdArgs = []string{"python3", "-m", "pytest", "-q"}
		if filter != "" {
			cmdArgs = append(cmdArgs, "-k", filter)
		}
		if testPath != "" {
			cmdArgs = append(cmdArgs, testPath)
		}
	default:
		return Result{Success: false, Error: fmt.Sprintf("unsupported framework: %s", framework)}
	}

	cmd := sandboxCmd(ctx, t.workspaceRoot, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = t.workspaceRoot
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	outStr := strings.TrimSpace(buf.String())
	if outStr == "" && err != nil {
		return Result{Success: false, Error: fmt.Sprintf("%s: %v", framework, err)}
	}
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("%s tests failed", framework), Output: outStr}
	}
	return Result{Success: true, Output: fmt.Sprintf("%s tests passed.\n%s", framework, outStr)}
}

func detectTestFramework(root string) string {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(root, "Cargo.toml")); err == nil {
		return "cargo"
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		data, _ := os.ReadFile(filepath.Join(root, "package.json"))
		s := string(data)
		if strings.Contains(s, "\"vitest\"") {
			return "vitest"
		}
		if strings.Contains(s, "\"jest\"") {
			return "jest"
		}
		return "vitest" // default for JS projects
	}
	if _, err := os.Stat(filepath.Join(root, "pyproject.toml")); err == nil {
		return "pytest"
	}
	return ""
}
