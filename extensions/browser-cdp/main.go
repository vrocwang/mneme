// Browser CDP extension for Mneme.
//
// Provides browser automation tools via Chrome DevTools Protocol:
//   - browser: navigate and extract content from web pages using a real browser
//   - screenshot: capture a full-page screenshot of a URL
//   - web_fetch: fetch and clean web content with JS rendering
//   - curl: raw HTTP request with full control over method, headers, and body
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

// ── JSON-RPC message types ──────────────────────────────────────────────

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

// ── Extension manifest ─────────────────────────────────────────────────────

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
	Name:        "browser-cdp",
	Version:     "0.1.0",
	Description: "CDP-based browser automation: browser, screenshot, web_fetch, curl",
	Tools:       []string{"browser", "screenshot", "web_fetch", "curl"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "browser",
		Description: "Navigate to a URL using a real browser and extract the rendered page content as readable text. Use this when you need JavaScript-rendered content or complex pages.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":     map[string]interface{}{"type": "string", "description": "URL to navigate to"},
				"timeout": map[string]interface{}{"type": "number", "description": "Page load timeout in seconds (default 15)"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "screenshot",
		Description: "Take a full-page screenshot of a URL using headless Chrome. Returns the screenshot as a base64-encoded PNG.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":      map[string]interface{}{"type": "string", "description": "URL to capture"},
				"width":    map[string]interface{}{"type": "number", "description": "Viewport width (default 1280)"},
				"height":   map[string]interface{}{"type": "number", "description": "Viewport height (default 800)"},
				"fullPage": map[string]interface{}{"type": "boolean", "description": "Capture full page (default true)"},
				"selector": map[string]interface{}{"type": "string", "description": "CSS selector to capture a specific element"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "web_fetch",
		Description: "Fetch and clean content from a web page, removing ads, navigation, and boilerplate. Returns the main content as readable text.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":         map[string]interface{}{"type": "string", "description": "URL to fetch"},
				"maxChars":    map[string]interface{}{"type": "number", "description": "Maximum characters to return (default 10000)"},
				"includeHTML": map[string]interface{}{"type": "boolean", "description": "Include raw HTML in output (default false)"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "curl",
		Description: "Make a raw HTTP request with full control over method, headers, and body. Use for API calls and debugging.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":     map[string]interface{}{"type": "string", "description": "URL to request"},
				"method":  map[string]interface{}{"type": "string", "description": "HTTP method (GET, POST, PUT, DELETE, etc.)"},
				"headers": map[string]interface{}{"type": "object", "description": "HTTP headers as key-value pairs"},
				"body":    map[string]interface{}{"type": "string", "description": "Request body"},
				"timeout": map[string]interface{}{"type": "number", "description": "Request timeout in seconds (default 30)"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

// ── Main ────────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("browser-cdp extension starting", "version", extManifest.Version)

	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout
	// Initialize tools
	cancel := initCDP()
	defer cancel()

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

		if req.JSONRPC != "2.0" {
			log.Warn("non-JSON-RPC 2.0 message, skipping")
			continue
		}

		resp := handleRequest(&req)
		respBytes, err := json.Marshal(resp)
		if err != nil {
			log.Error("marshal response error", "err", err)
			continue
		}
		fmt.Fprintf(writer, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		return handleDescribe(req)
	case "extension.list_tools":
		return handleListTools(req)
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		return handleCallTool(req)
	default:
		return &rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)},
		}
	}
}

func handleDescribe(req *rpcRequest) *rpcResponse {
	result, _ := json.Marshal(extManifest)
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func handleListTools(req *rpcRequest) *rpcResponse {
	type listResult struct {
		Tools []toolDef `json:"tools"`
	}
	result, _ := json.Marshal(listResult{Tools: toolDefs})
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func handleCallTool(req *rpcRequest) *rpcResponse {
	var params callToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32602, Message: fmt.Sprintf("invalid params: %v", err)},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var result callToolResult
	switch params.Name {
	case "browser":
		result = browserTool(ctx, params.Args)
	case "screenshot":
		result = screenshotTool(ctx, params.Args)
	case "web_fetch":
		result = webFetchTool(ctx, params.Args)
	case "curl":
		result = curlTool(ctx, params.Args)
	default:
		result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
	}

	resultBytes, _ := json.Marshal(result)
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
}
