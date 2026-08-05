// Channel QQ extension for Mneme.
//
// Provides QQ (Tencent) messaging integration tools via QQ Bot API:
//   - qq_send_message: send a message to a QQ channel/group
//   - qq_status: check QQ Bot API connection status
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
	Name string
	Args map[string]interface{}
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

var extManifest = manifest{
	Name:        "channel-qq",
	Version:     "0.1.0",
	Description: "QQ channel: send messages via QQ Bot API",
	Tools:       []string{"qq_send_message", "qq_status"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "qq_send_message",
		Description: "Send a message to a QQ channel or group via the QQ Bot API. Requires QQ_BOT_APP_ID and QQ_BOT_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channelId": map[string]interface{}{"type": "string", "description": "Target QQ channel ID or group openid"},
				"content":   map[string]interface{}{"type": "string", "description": "Message content"},
				"msgType":   map[string]interface{}{"type": "string", "description": "Message type: text, markdown, image (default: text)"},
			},
			"required": []string{"channelId", "content"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "qq_status",
		Description: "Check QQ Bot API connection status",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	},
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("channel-qq extension starting")
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
		case "qq_send_message":
			result = qqSendMsg(ctx, params.Args)
		case "qq_status":
			result = qqStatus(ctx)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func getQQCreds() (appID, token string, err error) {
	appID = os.Getenv("QQ_BOT_APP_ID")
	token = os.Getenv("QQ_BOT_TOKEN")
	if appID == "" || token == "" {
		return "", "", fmt.Errorf("QQ_BOT_APP_ID and QQ_BOT_TOKEN must be set")
	}
	return appID, token, nil
}

func qqSendMsg(ctx context.Context, args map[string]interface{}) callToolResult {
	appID, botToken, err := getQQCreds()
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	channelID, _ := args["channelId"].(string)
	content, _ := args["content"].(string)
	msgType, _ := args["msgType"].(string)
	if msgType == "" {
		msgType = "text"
	}
	if channelID == "" || content == "" {
		return callToolResult{Error: "channelId and content are required"}
	}

	payload := map[string]interface{}{
		"content":  content,
		"msg_type": msgType,
	}
	if msgType == "markdown" {
		payload["markdown"] = map[string]string{"content": content}
	}

	b, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.sgroup.qq.com/channels/%s/messages", channelID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bot %s.%s", appID, botToken))

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("qq send: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func qqStatus(ctx context.Context) callToolResult {
	appID, _, err := getQQCreds()
	if err != nil {
		return callToolResult{Success: false, Error: fmt.Sprintf("QQ Bot API not configured: %v", err)}
	}

	url := fmt.Sprintf("https://api.sgroup.qq.com/users/@me")
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bot %s.%s", appID, os.Getenv("QQ_BOT_TOKEN")))
	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("status: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 400 {
		return callToolResult{Success: true, Output: fmt.Sprintf("QQ Bot API connected\nApp ID: %s\n%s", appID, string(rbody))}
	}
	return callToolResult{Error: fmt.Sprintf("API check failed (status %d): %s", resp.StatusCode, string(rbody))}
}
