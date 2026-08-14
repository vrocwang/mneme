// Runtime Tools extension for Mneme.
//
// Provides language runtime execution tools:
//   - node_exec: execute JavaScript/TypeScript code via Node.js
//   - npm_exec: run npm commands
//   - python_exec: execute Python code
//   - python_venv: manage Python virtual environments
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "runtime-tools",
		Version:     "0.1.0",
		Description: "Language runtime tools: node_exec, npm_exec, python_exec, python_venv",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "node_exec",
		Description: "Execute JavaScript/TypeScript code using Node.js. The code is written to a temp file and executed.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":    map[string]interface{}{"type": "string", "description": "JavaScript/TypeScript code to execute"},
				"workDir": map[string]interface{}{"type": "string", "description": "Working directory for execution"},
				"timeout": map[string]interface{}{"type": "number", "description": "Timeout in seconds (default 30)"},
			},
			"required": []string{"code"},
		},
		Permission: "execute",
		HasEffects: true,
	}, nodeExec)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "npm_exec",
		Description: "Execute an npm command (install, run, test, etc.)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{"type": "string", "description": "npm command with args (e.g. 'install express', 'run build')"},
				"workDir": map[string]interface{}{"type": "string", "description": "Working directory"},
				"timeout": map[string]interface{}{"type": "number", "description": "Timeout in seconds (default 120)"},
			},
			"required": []string{"command"},
		},
		Permission: "execute",
		HasEffects: true,
	}, npmExec)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "python_exec",
		Description: "Execute Python code. Writes code to a temp file and runs it with python3.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":    map[string]interface{}{"type": "string", "description": "Python code to execute"},
				"workDir": map[string]interface{}{"type": "string", "description": "Working directory"},
				"timeout": map[string]interface{}{"type": "number", "description": "Timeout in seconds (default 30)"},
				"venv":    map[string]interface{}{"type": "string", "description": "Path to virtual environment to use"},
			},
			"required": []string{"code"},
		},
		Permission: "execute",
		HasEffects: true,
	}, pythonExec)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "python_venv",
		Description: "Create or manage a Python virtual environment",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "description": "Action: create, list, activate, install"},
				"path":     map[string]interface{}{"type": "string", "description": "Path for the venv (for create)"},
				"packages": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Packages to install (pip install)"},
			},
			"required": []string{"action"},
		},
		Permission: "execute",
		HasEffects: true,
	}, pythonVenv)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "runtime-tools: %v\n", err)
		os.Exit(1)
	}
}

func nodeExec(ctx context.Context, args map[string]interface{}) extsdk.Result {
	code, _ := args["code"].(string)
	if code == "" {
		return extsdk.Result{Error: "code is required"}
	}
	workDir, _ := args["workDir"].(string)
	if workDir == "" {
		workDir = os.TempDir()
	}

	if _, err := exec.LookPath("node"); err != nil {
		return extsdk.Result{Error: "node not found. Install Node.js: https://nodejs.org"}
	}

	timeout := 30
	if t, ok := getInt(args, "timeout"); ok && t > 0 {
		timeout = t
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("oh-node-%d.js", time.Now().UnixMilli()))
	os.WriteFile(scriptPath, []byte(code), 0644)
	defer os.Remove(scriptPath)

	cmd := exec.CommandContext(ctx, "node", scriptPath)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()

	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("node: %v\n%s", err, string(out))}
	}
	return extsdk.Result{Success: true, Output: string(out)}
}

func npmExec(ctx context.Context, args map[string]interface{}) extsdk.Result {
	command, _ := args["command"].(string)
	if command == "" {
		return extsdk.Result{Error: "command is required"}
	}
	workDir, _ := args["workDir"].(string)
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	npmPath := "npm"
	if runtime.GOOS == "windows" {
		npmPath = "npm.cmd"
	}
	if _, err := exec.LookPath(npmPath); err != nil {
		return extsdk.Result{Error: "npm not found. Install Node.js: https://nodejs.org"}
	}

	timeout := 120
	if t, ok := getInt(args, "timeout"); ok && t > 0 {
		timeout = t
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	parts := strings.Fields(command)
	cmd := exec.CommandContext(ctx, npmPath, parts...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()

	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("npm: %v\n%s", err, string(out))}
	}
	return extsdk.Result{Success: true, Output: string(out)}
}

func pythonExec(ctx context.Context, args map[string]interface{}) extsdk.Result {
	code, _ := args["code"].(string)
	if code == "" {
		return extsdk.Result{Error: "code is required"}
	}
	workDir, _ := args["workDir"].(string)
	if workDir == "" {
		workDir = os.TempDir()
	}

	python := "python3"
	if _, err := exec.LookPath(python); err != nil {
		if _, err2 := exec.LookPath("python"); err2 == nil {
			python = "python"
		}
		if python == "python3" {
			return extsdk.Result{Error: "python3 not found"}
		}
	}

	timeout := 30
	if t, ok := getInt(args, "timeout"); ok && t > 0 {
		timeout = t
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("oh-python-%d.py", time.Now().UnixMilli()))
	os.WriteFile(scriptPath, []byte(code), 0644)
	defer os.Remove(scriptPath)

	execPath := python
	if venv, ok := args["venv"].(string); ok && venv != "" {
		execPath = filepath.Join(venv, "bin", "python3")
	}

	cmd := exec.CommandContext(ctx, execPath, scriptPath)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()

	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("python: %v\n%s", err, string(out))}
	}
	return extsdk.Result{Success: true, Output: string(out)}
}

func pythonVenv(ctx context.Context, args map[string]interface{}) extsdk.Result {
	action, _ := args["action"].(string)
	if action == "" {
		return extsdk.Result{Error: "action is required"}
	}

	python := "python3"
	if _, err := exec.LookPath(python); err != nil {
		if _, err2 := exec.LookPath("python"); err2 == nil {
			python = "python"
		}
	}

	switch action {
	case "create":
		venvPath, _ := args["path"].(string)
		if venvPath == "" {
			venvPath = filepath.Join(os.TempDir(), "oh-venv")
		}
		cmd := exec.CommandContext(ctx, python, "-m", "venv", venvPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("venv create: %v\n%s", err, string(out))}
		}
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Virtual environment created: %s", venvPath)}

	case "install":
		venvPath, _ := args["path"].(string)
		if venvPath == "" {
			return extsdk.Result{Error: "path is required for install action"}
		}
		packages := getStrSlice(args, "packages")
		if len(packages) == 0 {
			return extsdk.Result{Error: "packages array is required for install"}
		}

		pipPath := filepath.Join(venvPath, "bin", "pip3")
		if runtime.GOOS == "windows" {
			pipPath = filepath.Join(venvPath, "Scripts", "pip.exe")
		}

		pipArgs := append([]string{"install"}, packages...)
		cmd := exec.CommandContext(ctx, pipPath, pipArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("pip install: %v\n%s", err, string(out))}
		}
		return extsdk.Result{Success: true, Output: string(out)}

	case "list":
		entries, _ := os.ReadDir(os.TempDir())
		var venvs []string
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "oh-venv") {
				venvs = append(venvs, filepath.Join(os.TempDir(), e.Name()))
			}
		}
		if len(venvs) == 0 {
			return extsdk.Result{Success: true, Output: "No virtual environments found."}
		}
		b, _ := json.MarshalIndent(venvs, "", "  ")
		return extsdk.Result{Success: true, Output: string(b)}

	default:
		return extsdk.Result{Error: fmt.Sprintf("unknown action: %s (valid: create, list, install)", action)}
	}
}

func getInt(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func getStrSlice(args map[string]interface{}, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, a := range arr {
		if s, ok := a.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
