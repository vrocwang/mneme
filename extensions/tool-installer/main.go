// Tool Installer extension for Mneme.
//
// Provides:
//   - install_tool: detect OS and install system packages via apt, brew, pip, npm, or go
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	AgentDefs   []string `json:"agent_defs"`
	ProtocolMin int      `json:"protocol_min"`
}
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}
type callToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

var extManifest = manifest{
	Name:        "tool-installer",
	Version:     "0.1.0",
	Description: "Detect OS and install system packages via apt, brew, pip, npm, or go",
	Tools:       []string{"install_tool"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
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
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("tool-installer extension starting")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		var req rpcRequest
		json.Unmarshal(line, &req)
		resp := handleRequest(&req)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		type lr struct{ Tools []toolDef }
		result, _ := json.Marshal(lr{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "install_tool":
			result = installTool(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func installTool(ctx context.Context, args map[string]interface{}) callToolResult {
	name, _ := args["name"].(string)
	if name == "" {
		return callToolResult{Error: "name is required"}
	}
	if strings.HasPrefix(name, "-") {
		return callToolResult{Error: "invalid package name: names starting with '-' are not allowed"}
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
		return callToolResult{Error: fmt.Sprintf("unsupported method: %s (use: apt, brew, pip, npm, go)", method)}
	}

	// Check if the command exists
	if _, err := exec.LookPath(cmd.Path); err != nil {
		return callToolResult{Error: fmt.Sprintf("command %q not found on this system", cmd.Path)}
	}

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		return callToolResult{Success: false, Output: outStr, Error: fmt.Sprintf("install failed: %v", err)}
	}

	result := map[string]interface{}{
		"os":      runtime.GOOS + "/" + runtime.GOARCH,
		"method":  method,
		"package": name,
		"status":  "installed",
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return callToolResult{Success: true, Output: string(b) + "\n\n" + outStr}
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
