// GitBooks extension for Mneme.
//
// Provides:
//   - gitbooks_search: search GitBooks documentation via the GitBook API
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
	"net/http"
	"net/url"
	"os"
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
	Name:        "tool-gitbooks",
	Version:     "0.1.0",
	Description: "Search GitBooks documentation via the GitBook API",
	Tools:       []string{"gitbooks_search"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "gitbooks_search",
		Description: "Search GitBooks documentation via the GitBook API",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":    map[string]interface{}{"type": "string", "description": "Search query string"},
				"space_id": map[string]interface{}{"type": "string", "description": "GitBook space ID (optional, falls back to GITBOOK_SPACE_ID env var)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("tool-gitbooks extension starting")
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
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "gitbooks_search":
			result = gitbooksSearch(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func gitbooksSearch(ctx context.Context, args map[string]interface{}) callToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return callToolResult{Error: "query is required"}
	}

	spaceID, _ := args["space_id"].(string)
	if spaceID == "" {
		spaceID = os.Getenv("GITBOOK_SPACE_ID")
	}
	if spaceID == "" {
		return callToolResult{Error: "space_id is required (or set GITBOOK_SPACE_ID env var)"}
	}

	apiURL := fmt.Sprintf("https://api.gitbook.com/v1/spaces/%s/search?query=%s",
		url.PathEscape(spaceID), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("request: %v", err)}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("search: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("read response: %v", err)}
	}
	if resp.StatusCode >= 400 {
		return callToolResult{Error: fmt.Sprintf("API error %d: %s", resp.StatusCode, truncate(string(body), 500))}
	}

	var pretty interface{}
	if err := json.Unmarshal(body, &pretty); err != nil {
		return callToolResult{Success: true, Output: truncate(string(body), 4000)}
	}
	b, _ := json.MarshalIndent(pretty, "", "  ")
	return callToolResult{Success: true, Output: truncate(string(b), 4000)}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}
