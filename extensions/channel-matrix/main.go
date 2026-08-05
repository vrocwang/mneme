// Channel Matrix extension for Mneme.
//
// Provides Matrix messaging integration tools:
//   - matrix_send: send a message to a Matrix room
//   - matrix_join: join a Matrix room
//   - matrix_sync: sync recent messages from joined rooms
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"bytes"
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
	Name:        "channel-matrix",
	Version:     "0.1.0",
	Description: "Matrix channel: send messages, join rooms, sync",
	Tools:       []string{"matrix_send", "matrix_join", "matrix_sync"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "matrix_send",
		Description: "Send a message to a Matrix room. Requires MATRIX_HOMESERVER, MATRIX_ACCESS_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"roomId": map[string]interface{}{"type": "string", "description": "Matrix room ID (e.g. !abc123:matrix.org)"},
				"body":   map[string]interface{}{"type": "string", "description": "Message body (plain text)"},
				"format": map[string]interface{}{"type": "string", "description": "Message format: plain, markdown (via formatted_body)"},
			},
			"required": []string{"roomId", "body"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "matrix_join",
		Description: "Join a Matrix room by ID or alias. Requires MATRIX_HOMESERVER, MATRIX_ACCESS_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"roomIdOrAlias": map[string]interface{}{"type": "string", "description": "Room ID (!xxx:server) or alias (#room:server)"},
			},
			"required": []string{"roomIdOrAlias"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "matrix_sync",
		Description: "Sync recent messages from the Matrix homeserver. Requires MATRIX_HOMESERVER, MATRIX_ACCESS_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"since":  map[string]interface{}{"type": "string", "description": "Sync token from previous sync (optional)"},
				"filter": map[string]interface{}{"type": "string", "description": "Event filter JSON (optional)"},
				"limit":  map[string]interface{}{"type": "number", "description": "Max events per room (default 10)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("channel-matrix extension starting")
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
		type listResult struct {
			Tools []toolDef `json:"tools"`
		}
		result, _ := json.Marshal(listResult{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		type listAgentsResult struct {
			Agents []interface{} `json:"agents"`
		}
		result, _ := json.Marshal(listAgentsResult{Agents: []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "matrix_send":
			result = matrixSend(ctx, params.Args)
		case "matrix_join":
			result = matrixJoin(ctx, params.Args)
		case "matrix_sync":
			result = matrixSync(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func getMatrixConfig() (homeserver, token string, err error) {
	homeserver = os.Getenv("MATRIX_HOMESERVER")
	token = os.Getenv("MATRIX_ACCESS_TOKEN")
	if homeserver == "" || token == "" {
		return "", "", fmt.Errorf("MATRIX_HOMESERVER and MATRIX_ACCESS_TOKEN must be set")
	}
	return homeserver, token, nil
}

func matrixSend(ctx context.Context, args map[string]interface{}) callToolResult {
	hs, token, err := getMatrixConfig()
	if err != nil {
		return callToolResult{Error: err.Error()}
	}
	roomID, _ := args["roomId"].(string)
	body, _ := args["body"].(string)
	if roomID == "" || body == "" {
		return callToolResult{Error: "roomId and body are required"}
	}

	txnID := fmt.Sprintf("oh_%d", time.Now().UnixMilli())
	payload := map[string]interface{}{
		"msgtype": "m.text",
		"body":    body,
	}
	if format, _ := args["format"].(string); format == "markdown" {
		payload["format"] = "org.matrix.custom.html"
		payload["formatted_body"] = body
	}

	b, _ := json.Marshal(payload)
	reqURL := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		hs, url.PathEscape(roomID), url.PathEscape(txnID))
	req, _ := http.NewRequestWithContext(ctx, "PUT", reqURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("matrix send: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func matrixJoin(ctx context.Context, args map[string]interface{}) callToolResult {
	hs, token, err := getMatrixConfig()
	if err != nil {
		return callToolResult{Error: err.Error()}
	}
	roomID, _ := args["roomIdOrAlias"].(string)
	if roomID == "" {
		return callToolResult{Error: "roomIdOrAlias is required"}
	}

	reqURL := fmt.Sprintf("%s/_matrix/client/v3/join/%s", hs, url.PathEscape(roomID))
	req, _ := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("matrix join: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func matrixSync(ctx context.Context, args map[string]interface{}) callToolResult {
	hs, token, err := getMatrixConfig()
	if err != nil {
		return callToolResult{Error: err.Error()}
	}
	since, _ := args["since"].(string)
	filter, _ := args["filter"].(string)
	limit := 10
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}

	reqURL := fmt.Sprintf("%s/_matrix/client/v3/sync?timeout=5000", hs)
	if since != "" {
		reqURL += "&since=" + url.QueryEscape(since)
	}
	if filter != "" {
		reqURL += "&filter=" + url.QueryEscape(filter)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("matrix sync: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]struct {
				Timeline struct {
					Events []struct {
						Type    string `json:"type"`
						Sender  string `json:"sender"`
						Content struct {
							Body string `json:"body"`
						} `json:"content"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	json.Unmarshal(body, &data)

	var out string
	out += fmt.Sprintf("Sync token: %s\n\n", data.NextBatch)
	for roomID, room := range data.Rooms.Join {
		out += fmt.Sprintf("Room: %s\n", roomID)
		count := 0
		for _, ev := range room.Timeline.Events {
			if ev.Type == "m.room.message" && count < limit {
				out += fmt.Sprintf("  [%s] %s\n", ev.Sender, ev.Content.Body)
				count++
			}
		}
	}
	return callToolResult{Success: true, Output: out}
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
