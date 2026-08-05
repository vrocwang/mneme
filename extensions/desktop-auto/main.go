// Desktop Auto extension for Mneme.
//
// Provides desktop automation tools:
//   - mouse: move, click, drag, scroll
//   - keyboard: type text, press key combinations
//   - automate: run sequences of mouse/keyboard actions
//   - ax_interact: interact with UI elements via accessibility tree
//   - launch_app: launch desktop applications
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
	"time"
)

// ── JSON-RPC types ─────────────────────────────────────────────────────

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

type agentDef struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	SystemPrompt  string   `json:"systemPrompt"`
	Tier          string   `json:"tier"`
	ToolAllowlist []string `json:"toolAllowlist"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`
}

var extManifest = manifest{
	Name:        "desktop-auto",
	Version:     "0.1.0",
	Description: "Desktop automation: mouse, keyboard, automate, ax_interact, launch_app",
	Tools:       []string{"mouse", "keyboard", "automate", "ax_interact", "launch_app"},
	AgentDefs:   []string{"desktop_control_agent"},
	ProtocolMin: 1,
}

var extAgentDefs = []agentDef{
	{
		ID:          "desktop_control_agent",
		Name:        "Desktop Controller",
		Description: "Automates desktop interactions: mouse, keyboard, accessibility tree navigation, and app launching",
		Tier:        "worker",
		SystemPrompt: `You are a desktop automation specialist. Use mouse, keyboard, and accessibility tools to control the desktop.
- Verify element visibility before interacting
- Add pauses between actions to allow UI to respond
- Handle errors gracefully — report what failed and why`,
		ToolAllowlist: []string{"mouse", "keyboard", "automate", "ax_interact", "launch_app", "read_file", "shell"},
		MaxIterations: 12,
		Hidden:        false,
	},
}

var toolDefs = []toolDef{
	{
		Name:        "mouse",
		Description: "Control the mouse: move to coordinates, click (left/right/middle), double-click, drag, scroll",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "description": "Action: move, click, dblclick, rightclick, drag, scroll_up, scroll_down"},
				"x":      map[string]interface{}{"type": "number", "description": "X coordinate (for move/click/drag)"},
				"y":      map[string]interface{}{"type": "number", "description": "Y coordinate (for move/click/drag)"},
				"dx":     map[string]interface{}{"type": "number", "description": "Delta X (for drag)"},
				"dy":     map[string]interface{}{"type": "number", "description": "Delta Y (for drag)"},
				"amount": map[string]interface{}{"type": "number", "description": "Scroll amount (for scroll)"},
			},
			"required": []string{"action"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "keyboard",
		Description: "Control the keyboard: type text, press key combinations (ctrl+c, alt+tab, etc.)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "description": "Action: type, combo"},
				"text":   map[string]interface{}{"type": "string", "description": "Text to type (for type action)"},
				"keys":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Keys for combo: [\"ctrl\", \"c\"] or [\"alt\", \"tab\"]"},
			},
			"required": []string{"action"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "automate",
		Description: "Run a sequence of mouse and keyboard actions from a JSON plan",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"plan":    map[string]interface{}{"type": "string", "description": "JSON array of action objects, each with action and parameters"},
				"delayMs": map[string]interface{}{"type": "number", "description": "Delay between actions in milliseconds (default 100)"},
			},
			"required": []string{"plan"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "ax_interact",
		Description: "Interact with UI elements via the accessibility tree: list elements, get properties, perform actions (press, increment, decrement)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "description": "Action: list, get, press, focus"},
				"target":   map[string]interface{}{"type": "string", "description": "Element description or accessibility identifier"},
				"appName":  map[string]interface{}{"type": "string", "description": "Application name to scope the search"},
				"maxItems": map[string]interface{}{"type": "number", "description": "Max items to return (default 20)"},
			},
			"required": []string{"action"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "launch_app",
		Description: "Launch a desktop application by name or path",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Application name (e.g. 'firefox', 'code', 'terminal')"},
				"path": map[string]interface{}{"type": "string", "description": "Full path to executable (alternative to name)"},
				"args": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Command line arguments"},
				"wait": map[string]interface{}{"type": "boolean", "description": "Wait for process to finish (default false)"},
			},
			"required": []string{},
		},
		Permission: "execute",
		HasEffects: true,
	},
}

// ── Main ──────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("desktop-auto extension starting", "version", extManifest.Version)

	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				log.Info("stdin closed, exiting")
				return
			}
			log.Error("read error", "err", err)
			return
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Error("unmarshal error", "err", err)
			continue
		}

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
		type listResult struct {
			Tools []toolDef `json:"tools"`
		}
		result, _ := json.Marshal(listResult{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		type listAgentsResult struct {
			Agents []agentDef `json:"agents"`
		}
		result, _ := json.Marshal(listAgentsResult{Agents: extAgentDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		return handleCallTool(req)
	default:
		return &rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)},
		}
	}
}

func handleCallTool(req *rpcRequest) *rpcResponse {
	var params callToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32602, Message: fmt.Sprintf("invalid params: %v", err)},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result callToolResult
	switch params.Name {
	case "mouse":
		result = mouseTool(ctx, params.Args)
	case "keyboard":
		result = keyboardTool(ctx, params.Args)
	case "automate":
		result = automateTool(ctx, params.Args)
	case "ax_interact":
		result = axInteractTool(ctx, params.Args)
	case "launch_app":
		result = launchAppTool(ctx, params.Args)
	default:
		result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
	}

	resultBytes, _ := json.Marshal(result)
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
}
