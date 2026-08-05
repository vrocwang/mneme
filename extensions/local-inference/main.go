// Local Inference extension for Mneme.
//
// Manages the lifecycle of local inference services (Ollama, LM Studio):
// start, stop, health check, model pull, and model listing. Communicates
// via the Mneme extension protocol (stdin/stdout JSON-RPC 2.0).
//
// Protocol methods:
//
//	extension.describe      → returns manifest
//	extension.list_tools    → returns tool definitions
//	extension.list_agents   → returns agent definitions
//	extension.call_tool     → executes a tool
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ── Extension protocol types ───────────────────────────────────────────────

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

// ── Extension metadata ──────────────────────────────────────────────────────

var extManifest = manifest{
	Name:        "local-inference",
	Version:     "0.1.0",
	Description: "Local inference service lifecycle (Ollama, LM Studio)",
	Tools:       []string{"inference_start", "inference_stop", "inference_health_check", "inference_pull_model", "inference_list_models"},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "inference_start",
		Description: "Start a local inference service (Ollama or LM Studio).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service": map[string]interface{}{"type": "string", "description": "Service to start: ollama or lmstudio (default: ollama)"},
			},
			"required": []string{},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "inference_stop",
		Description: "Stop a running local inference service.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service": map[string]interface{}{"type": "string", "description": "Service to stop (default: ollama)"},
			},
			"required": []string{},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "inference_health_check",
		Description: "Check if a local inference service is running and healthy.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service": map[string]interface{}{"type": "string", "description": "Service to check (default: ollama)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "inference_pull_model",
		Description: "Pull/download a model for local inference.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"model": map[string]interface{}{"type": "string", "description": "Model name (e.g. llama3, mistral, codellama)"},
			},
			"required": []string{"model"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "inference_list_models",
		Description: "List locally available models.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

var agentDefs = []struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"systemPrompt"`
	ToolAllowlist []string `json:"toolAllowlist"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`
}{
	{
		ID:          "inference_manager",
		Name:        "Inference Manager",
		Description: "Manages local inference services: start, stop, pull models, health checks",
		Tier:        "worker",
		SystemPrompt: `You manage local inference services like Ollama and LM Studio.
	- Start/stop services on demand
	- Pull models when requested
	- Check service health
	- List available models`,
		ToolAllowlist: []string{"inference_start", "inference_stop", "inference_health_check", "inference_pull_model", "inference_list_models"},
		MaxIterations: 5,
		Hidden:        false,
	},
}

// ── Default endpoints ────────────────────────────────────────────────────

const (
	ollamaBaseURL   = "http://127.0.0.1:11434"
	lmstudioBaseURL = "http://127.0.0.1:1234"
)

func serviceURL(service string) string {
	switch strings.ToLower(service) {
	case "lmstudio", "lm-studio", "lm_studio":
		return lmstudioBaseURL
	default:
		return ollamaBaseURL
	}
}

func serviceCommand(service string) string {
	switch strings.ToLower(service) {
	case "lmstudio", "lm-studio", "lm_studio":
		return "lm-studio"
	default:
		return "ollama"
	}
}

// ── Main loop ────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("local-inference extension starting")
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
		result, _ := json.Marshal(map[string]interface{}{"tools": toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": agentDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "inference_start":
			result = startService(ctx, params.Args)
		case "inference_stop":
			result = stopService(params.Args)
		case "inference_health_check":
			result = healthCheck(ctx, params.Args)
		case "inference_pull_model":
			result = pullModel(ctx, params.Args)
		case "inference_list_models":
			result = listModels(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
		}
		res, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)}}
	}
}

// ── Tool implementations ─────────────────────────────────────────────────

func startService(ctx context.Context, args map[string]interface{}) callToolResult {
	service := getStrArg(args, "service", "ollama")
	cmdName := serviceCommand(service)

	if _, err := exec.LookPath(cmdName); err != nil {
		return callToolResult{Error: fmt.Sprintf("%s not found in PATH. Install it first.", cmdName)}
	}

	// Check if already running.
	if isHealthy(ctx, serviceURL(service)) {
		return callToolResult{Success: true, Output: fmt.Sprintf("%s is already running.", service)}
	}

	cmd := exec.CommandContext(ctx, cmdName, "serve")
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return callToolResult{Error: fmt.Sprintf("start %s: %v", service, err)}
	}

	// Track the PID for targeted shutdown.
	servicePid = cmd.Process.Pid

	// Reap the process in the background to prevent zombies when it exits.
	go func() { cmd.Wait(); servicePid = 0 }()

	// Wait briefly for the service to become healthy.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if isHealthy(ctx, serviceURL(service)) {
			return callToolResult{Success: true, Output: fmt.Sprintf("%s started successfully.", service)}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return callToolResult{Success: true, Output: fmt.Sprintf("%s process started (pid %d). Health check pending.", service, cmd.Process.Pid)}
}

var (
	servicePid int // PID of the started service, tracked for targeted shutdown
)

func stopService(args map[string]interface{}) callToolResult {
	service := getStrArg(args, "service", "ollama")
	cmdName := serviceCommand(service)

	// If we have a tracked PID from startService, kill only that process.
	if servicePid > 0 {
		proc, err := os.FindProcess(servicePid)
		if err == nil {
			pid := servicePid
			if err := proc.Kill(); err == nil || strings.Contains(err.Error(), "already finished") || strings.Contains(err.Error(), "no such process") {
				servicePid = 0
				return callToolResult{Success: true, Output: fmt.Sprintf("%s stopped (pid %d).", service, pid)}
			}
		}
		servicePid = 0
	}

	// Fallback: use pkill with exact process name match.
	// Use -x to match exact process name, avoiding killing unrelated processes.
	out, err := exec.Command("pkill", "-x", cmdName).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no process") || strings.Contains(err.Error(), "exit status 1") {
			return callToolResult{Success: true, Output: fmt.Sprintf("%s was not running.", service)}
		}
		return callToolResult{Error: fmt.Sprintf("stop %s: %v (%s)", service, err, string(out))}
	}

	return callToolResult{Success: true, Output: fmt.Sprintf("%s stopped.", service)}
}

func healthCheck(ctx context.Context, args map[string]interface{}) callToolResult {
	service := getStrArg(args, "service", "ollama")
	url := serviceURL(service)

	if isHealthy(ctx, url) {
		return callToolResult{Success: true, Output: fmt.Sprintf("%s is healthy at %s", service, url)}
	}
	return callToolResult{Success: true, Output: fmt.Sprintf("%s is not reachable at %s", service, url)}
}

func pullModel(ctx context.Context, args map[string]interface{}) callToolResult {
	model := getStrArg(args, "model", "")
	if model == "" {
		return callToolResult{Error: "model name is required"}
	}
	// Currently only Ollama supports model pulling.

	if !isHealthy(ctx, ollamaBaseURL) {
		return callToolResult{Error: "ollama is not running. Start it first with inference_start."}
	}

	cmd := exec.CommandContext(ctx, "ollama", "pull", model)
	cmd.Stderr = os.Stderr
	out, err := cmd.CombinedOutput()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("pull %s: %v (%s)", model, err, string(out))}
	}

	return callToolResult{Success: true, Output: fmt.Sprintf("Model %s pulled successfully.\n%s", model, string(out))}
}

func listModels(ctx context.Context, args map[string]interface{}) callToolResult {
	svc := getStrArg(args, "service", "ollama")
	url := serviceURL(svc) + "/api/tags"

	if !isHealthy(ctx, serviceURL(svc)) {
		return callToolResult{Error: fmt.Sprintf("%s is not running.", svc)}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("create request: %v", err)}
	}
	resp, err := client.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("list models: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("read response: %v", err)}
	}

	// Pretty-print the JSON for readability.
	var parsed interface{}
	if json.Unmarshal(body, &parsed) == nil {
		pretty, _ := json.MarshalIndent(parsed, "", "  ")
		return callToolResult{Success: true, Output: string(pretty)}
	}
	return callToolResult{Success: true, Output: string(body)}
}

// ── Helpers ──────────────────────────────────────────────────────────────

func isHealthy(ctx context.Context, baseURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func getStrArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}
