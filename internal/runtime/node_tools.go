package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RuntimeToolSummary is the result of listing tools from a script runtime.
type RuntimeToolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Runtime     string `json:"runtime"` // "node", "python"
}

// ExecuteToolOutcome is the result of executing a tool via a script runtime.
type ExecuteToolOutcome struct {
	Success bool          `json:"success"`
	Output  string        `json:"output"`
	Error   string        `json:"error,omitempty"`
	Elapsed time.Duration `json:"elapsed_ms"`
}

// NodeToolBridge provides tool listing and execution via Node.js scripts.
type NodeToolBridge struct {
	nodeBinary string
}

// NewNodeToolBridge creates a tool bridge using the given node binary.
func NewNodeToolBridge(nodeBinary string) *NodeToolBridge {
	if nodeBinary == "" {
		nodeBinary = "node"
	}
	return &NodeToolBridge{nodeBinary: nodeBinary}
}

// ListTools executes a Node.js script that prints JSON array of {name, description}
// to stdout. The script path is typically an MCP server or skill entrypoint.
func (b *NodeToolBridge) ListTools(ctx context.Context, scriptPath string) ([]RuntimeToolSummary, error) {
	cmd := exec.CommandContext(ctx, b.nodeBinary, scriptPath, "--list-tools")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("node list-tools: %w\n%s", err, stderr.String())
	}

	var tools []RuntimeToolSummary
	if err := json.Unmarshal(stdout.Bytes(), &tools); err != nil {
		return nil, fmt.Errorf("parse tool list: %w\nraw: %s", err, stdout.String())
	}
	return tools, nil
}

// ExecuteTool runs a specific tool by name with arguments via Node.js.
func (b *NodeToolBridge) ExecuteTool(ctx context.Context, scriptPath, toolName string, args map[string]interface{}) (*ExecuteToolOutcome, error) {
	req := struct {
		Tool string                 `json:"tool"`
		Args map[string]interface{} `json:"args"`
	}{Tool: toolName, Args: args}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal execute request: %w", err)
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, b.nodeBinary, scriptPath, "--execute")
	cmd.Stdin = bytes.NewReader(reqJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	elapsed := time.Since(start)

	outcome := &ExecuteToolOutcome{
		Output:  strings.TrimSpace(stdout.String()),
		Elapsed: elapsed,
	}

	if runErr != nil {
		outcome.Error = fmt.Sprintf("%v\n%s", runErr, stderr.String())
		return outcome, runErr
	}

	// Try to parse as JSON outcome.
	var parsed ExecuteToolOutcome
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err == nil {
		parsed.Elapsed = elapsed
		return &parsed, nil
	}

	outcome.Success = runErr == nil
	return outcome, nil
}

// PythonToolBridge provides tool listing and execution via Python scripts.
type PythonToolBridge struct {
	pythonBinary string
}

// NewPythonToolBridge creates a Python tool execution bridge.
func NewPythonToolBridge(pythonBinary string) *PythonToolBridge {
	if pythonBinary == "" {
		pythonBinary = "python3"
	}
	return &PythonToolBridge{pythonBinary: pythonBinary}
}

// ListTools executes a Python script that prints JSON array of tools.
func (b *PythonToolBridge) ListTools(ctx context.Context, scriptPath string) ([]RuntimeToolSummary, error) {
	cmd := exec.CommandContext(ctx, b.pythonBinary, scriptPath, "--list-tools")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python list-tools: %w\n%s", err, stderr.String())
	}

	var tools []RuntimeToolSummary
	if err := json.Unmarshal(stdout.Bytes(), &tools); err != nil {
		return nil, fmt.Errorf("parse python tool list: %w", err)
	}
	return tools, nil
}

// ExecuteTool runs a Python tool by name with arguments.
func (b *PythonToolBridge) ExecuteTool(ctx context.Context, scriptPath, toolName string, args map[string]interface{}) (*ExecuteToolOutcome, error) {
	req := struct {
		Tool string                 `json:"tool"`
		Args map[string]interface{} `json:"args"`
	}{Tool: toolName, Args: args}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal execute request: %w", err)
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, b.pythonBinary, scriptPath, "--execute")
	cmd.Stdin = bytes.NewReader(reqJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	outcome := &ExecuteToolOutcome{
		Output:  strings.TrimSpace(stdout.String()),
		Elapsed: time.Since(start),
	}
	if runErr != nil {
		outcome.Error = fmt.Sprintf("%v\n%s", runErr, stderr.String())
		return outcome, runErr
	}

	var parsed ExecuteToolOutcome
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err == nil {
		parsed.Elapsed = time.Since(start)
		return &parsed, nil
	}

	outcome.Success = runErr == nil
	return outcome, nil
}
