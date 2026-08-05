// Channel Mattermost extension for Mneme.
//
// Provides Mattermost integration tools:
//   - mattermost_send: send message to a channel
//   - mattermost_webhook: send via incoming webhook
//   - mattermost_list_channels: list accessible channels
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
	Name:        "channel-mattermost",
	Version:     "0.1.0",
	Description: "Mattermost channel: send messages, webhooks, list channels",
	Tools:       []string{"mattermost_send", "mattermost_webhook", "mattermost_list_channels"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "mattermost_send",
		Description: "Send a message to a Mattermost channel via API. Requires MATTERMOST_URL and MATTERMOST_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channelId": map[string]interface{}{"type": "string", "description": "Channel ID"},
				"message":   map[string]interface{}{"type": "string", "description": "Message text (supports Markdown)"},
			},
			"required": []string{"channelId", "message"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "mattermost_webhook",
		Description: "Send a message via a Mattermost incoming webhook URL (simpler setup)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"webhookUrl": map[string]interface{}{"type": "string", "description": "Incoming webhook URL"},
				"text":       map[string]interface{}{"type": "string", "description": "Message text"},
				"username":   map[string]interface{}{"type": "string", "description": "Override username (optional)"},
				"channel":    map[string]interface{}{"type": "string", "description": "Override channel (optional)"},
			},
			"required": []string{"webhookUrl", "text"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "mattermost_list_channels",
		Description: "List channels accessible to the bot. Requires MATTERMOST_URL and MATTERMOST_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"teamId": map[string]interface{}{"type": "string", "description": "Team ID to list channels for"},
				"limit":  map[string]interface{}{"type": "number", "description": "Max results (default 50)"},
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
	log.Info("channel-mattermost extension starting")
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
		case "mattermost_send":
			result = mattermostSend(ctx, params.Args)
		case "mattermost_webhook":
			result = mattermostWebhook(ctx, params.Args)
		case "mattermost_list_channels":
			result = mattermostListChannels(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func getMattermostConfig() (url, token string, err error) {
	url = os.Getenv("MATTERMOST_URL")
	token = os.Getenv("MATTERMOST_TOKEN")
	if url == "" || token == "" {
		return "", "", fmt.Errorf("MATTERMOST_URL and MATTERMOST_TOKEN must be set")
	}
	return url, token, nil
}

func mattermostSend(ctx context.Context, args map[string]interface{}) callToolResult {
	baseURL, token, err := getMattermostConfig()
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	channelID, _ := args["channelId"].(string)
	message, _ := args["message"].(string)
	if channelID == "" || message == "" {
		return callToolResult{Error: "channelId and message are required"}
	}

	payload := map[string]interface{}{
		"channel_id": channelID,
		"message":    message,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/v4/posts", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("mattermost: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func mattermostWebhook(ctx context.Context, args map[string]interface{}) callToolResult {
	webhookURL, _ := args["webhookUrl"].(string)
	text, _ := args["text"].(string)
	if webhookURL == "" || text == "" {
		return callToolResult{Error: "webhookUrl and text are required"}
	}

	payload := map[string]interface{}{"text": text}
	if username, ok := args["username"].(string); ok && username != "" {
		payload["username"] = username
	}
	if channel, ok := args["channel"].(string); ok && channel != "" {
		payload["channel"] = channel
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(b))
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("webhook: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func mattermostListChannels(ctx context.Context, args map[string]interface{}) callToolResult {
	baseURL, token, err := getMattermostConfig()
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	teamID, _ := args["teamId"].(string)
	url := baseURL + "/api/v4/channels"
	if teamID != "" {
		url = baseURL + "/api/v4/teams/" + teamID + "/channels"
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("list channels: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(rbody)}
}
