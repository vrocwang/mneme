// Proxy Config extension for Mneme.
//
// Provides:
//   - proxy_config: view and set proxy configuration (HTTP/HTTPS)
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
	"path/filepath"
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
	Name:        "tool-proxy-config",
	Version:     "0.1.0",
	Description: "View and set HTTP/HTTPS proxy configuration",
	Tools:       []string{"proxy_config"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "proxy_config",
		Description: "View or set proxy configuration. Uses http_proxy/https_proxy environment variables and writes to shell config for persistence.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "description": "get or set"},
				"protocol": map[string]interface{}{"type": "string", "description": "http or https"},
				"host":     map[string]interface{}{"type": "string", "description": "Proxy host (required for set)"},
				"port":     map[string]interface{}{"type": "number", "description": "Proxy port (required for set)"},
			},
			"required": []string{"action"},
		},
		Permission: "execute",
		HasEffects: true,
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("tool-proxy-config extension starting")
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "proxy_config":
			result = proxyConfig(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func proxyConfig(_ context.Context, args map[string]interface{}) callToolResult {
	action, _ := args["action"].(string)

	if action == "get" || action == "" {
		// Read proxy from environment and shell config
		result := map[string]interface{}{
			"env": map[string]string{
				"http_proxy":  os.Getenv("http_proxy"),
				"https_proxy": os.Getenv("https_proxy"),
				"HTTP_PROXY":  os.Getenv("HTTP_PROXY"),
				"HTTPS_PROXY": os.Getenv("HTTPS_PROXY"),
				"no_proxy":    os.Getenv("no_proxy"),
				"NO_PROXY":    os.Getenv("NO_PROXY"),
			},
			"shell_config": readShellProxyConfig(),
			"os":           runtime.GOOS,
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return callToolResult{Success: true, Output: string(b)}
	}

	if action == "set" {
		protocol, _ := args["protocol"].(string)
		host, _ := args["host"].(string)
		port := 0
		if p, ok := getInt(args, "port"); ok {
			port = p
		}

		if protocol == "" {
			return callToolResult{Error: "protocol is required for set action (http or https)"}
		}
		if host == "" || port == 0 {
			return callToolResult{Error: "host and port are required for set action"}
		}

		proxyValue := fmt.Sprintf("http://%s:%d", host, port)
		envVar := strings.ToUpper(protocol) + "_PROXY"

		// Set in current process environment
		os.Setenv(envVar, proxyValue)
		os.Setenv(strings.ToLower(protocol)+"_proxy", proxyValue)

		// Write to shell config for persistence
		configFile := shellConfigPath()
		persisted := false
		if configFile != "" {
			if err := writeShellConfig(configFile, envVar, proxyValue); err != nil {
				return callToolResult{Error: fmt.Sprintf("write shell config: %v", err)}
			}
			persisted = true
		}

		result := map[string]interface{}{
			"action":    "set",
			"variable":  envVar,
			"value":     proxyValue,
			"persisted": persisted,
		}
		if configFile != "" {
			result["config_file"] = configFile
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return callToolResult{Success: true, Output: string(b)}
	}

	return callToolResult{Error: fmt.Sprintf("unknown action: %s (use get or set)", action)}
}

func shellConfigPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}

	// Detect which shell config file to use
	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "zsh") {
		return filepath.Join(home, ".zshrc")
	}
	if strings.Contains(shell, "bash") {
		// Prefer .bashrc on Linux
		if runtime.GOOS == "linux" {
			return filepath.Join(home, ".bashrc")
		}
		return filepath.Join(home, ".bash_profile")
	}
	// Default
	return filepath.Join(home, ".profile")
}

func readShellProxyConfig() map[string]string {
	configPath := shellConfigPath()
	if configPath == "" {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	proxy := make(map[string]string)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "export ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "export "), "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				if strings.Contains(strings.ToUpper(key), "PROXY") {
					proxy[key] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
				}
			}
		}
	}
	return proxy
}

func sanitizeShellValue(v string) string {
	// Escape backslash, double-quote, dollar-sign, and backtick for safe shell export.
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "$", "\\$")
	v = strings.ReplaceAll(v, "`", "\\`")
	return v
}

func writeShellConfig(configPath, envVar, value string) error {
	exportLine := fmt.Sprintf("export %s=\"%s\"", envVar, sanitizeShellValue(value))

	data, err := os.ReadFile(configPath)
	if err != nil {
		return os.WriteFile(configPath, []byte(exportLine+"\n"), 0644)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "export "+envVar+"=") {
			lines[i] = exportLine
			return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
		}
	}

	return os.WriteFile(configPath, []byte(content+"\n"+exportLine+"\n"), 0644)
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
