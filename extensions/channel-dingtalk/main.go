// Channel DingTalk extension for Mneme.
//
// Provides DingTalk messaging integration tools:
//   - dingtalk_send_message: send text/markdown message to a conversation
//   - dingtalk_send_webhook: send message via webhook robot URL
//   - dingtalk_get_token: get access token for API calls
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// ── JSON-RPC types ────────────────────────────────────────────────

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
	Name:        "channel-dingtalk",
	Version:     "0.1.0",
	Description: "DingTalk channel: send messages, webhooks, access token management",
	Tools:       []string{"dingtalk_send_message", "dingtalk_send_webhook", "dingtalk_get_token"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "dingtalk_send_message",
		Description: "Send a message to a DingTalk conversation via the Open API. Requires app credentials (appKey, appSecret).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"appKey":    map[string]interface{}{"type": "string", "description": "DingTalk app key"},
				"appSecret": map[string]interface{}{"type": "string", "description": "DingTalk app secret"},
				"userId":    map[string]interface{}{"type": "string", "description": "Recipient user ID (from DingTalk org)"},
				"msgType":   map[string]interface{}{"type": "string", "description": "Message type: text, markdown, link (default: text)"},
				"content":   map[string]interface{}{"type": "string", "description": "Message content"},
				"title":     map[string]interface{}{"type": "string", "description": "Title for markdown/link messages"},
			},
			"required": []string{"appKey", "appSecret", "userId", "content"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "dingtalk_send_webhook",
		Description: "Send a message to a DingTalk group via a custom robot webhook URL. Supports text, markdown, and link message types. Includes signature verification if secret is provided.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"webhookUrl": map[string]interface{}{"type": "string", "description": "DingTalk robot webhook URL"},
				"secret":     map[string]interface{}{"type": "string", "description": "Robot secret for signature (optional, required if robot has signature enabled)"},
				"msgType":    map[string]interface{}{"type": "string", "description": "Message type: text, markdown, link (default: text)"},
				"content":    map[string]interface{}{"type": "string", "description": "Message content"},
				"title":      map[string]interface{}{"type": "string", "description": "Title for markdown/link messages"},
				"picUrl":     map[string]interface{}{"type": "string", "description": "Image URL (for link messages)"},
				"messageUrl": map[string]interface{}{"type": "string", "description": "Click-through URL (for link messages)"},
			},
			"required": []string{"webhookUrl", "content"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "dingtalk_get_token",
		Description: "Get a DingTalk access token for API calls",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"appKey":    map[string]interface{}{"type": "string", "description": "DingTalk app key"},
				"appSecret": map[string]interface{}{"type": "string", "description": "DingTalk app secret"},
			},
			"required": []string{"appKey", "appSecret"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("channel-dingtalk extension starting", "version", extManifest.Version)

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
		case "dingtalk_send_message":
			result = dingtalkSendMsg(ctx, params.Args)
		case "dingtalk_send_webhook":
			result = dingtalkSendWebhook(ctx, params.Args)
		case "dingtalk_get_token":
			result = dingtalkGetToken(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

// ── DingTalk API ──────────────────────────────────────────────────

const dingtalkAPI = "https://api.dingtalk.com/v1.0"

type dingtalkTokenResp struct {
	AccessToken string `json:"accessToken"`
	ExpireIn    int64  `json:"expireIn"`
}

func dingtalkGetToken(ctx context.Context, args map[string]interface{}) callToolResult {
	appKey, _ := args["appKey"].(string)
	appSecret, _ := args["appSecret"].(string)

	req, _ := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/oauth2/accessToken", dingtalkAPI), nil)
	req.Header.Set("Content-Type", "application/json")

	body := map[string]string{
		"appKey":    appKey,
		"appSecret": appSecret,
	}
	b, _ := json.Marshal(body)
	req.Body = io.NopCloser(bytes.NewReader(b))
	req.ContentLength = int64(len(b))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("token request: %v", err)}
	}
	defer resp.Body.Close()

	rbody, _ := io.ReadAll(resp.Body)
	var tr dingtalkTokenResp
	json.Unmarshal(rbody, &tr)
	if tr.AccessToken == "" {
		return callToolResult{Error: fmt.Sprintf("failed to get token (status %d): %s", resp.StatusCode, string(rbody))}
	}
	return callToolResult{Success: true, Output: fmt.Sprintf("Access token: %s... (expires in %ds)", tr.AccessToken[:min(20, len(tr.AccessToken))], tr.ExpireIn)}
}

func dingtalkSendMsg(ctx context.Context, args map[string]interface{}) callToolResult {
	appKey, _ := args["appKey"].(string)
	appSecret, _ := args["appSecret"].(string)
	userID, _ := args["userId"].(string)
	content, _ := args["content"].(string)
	msgType, _ := args["msgType"].(string)
	title, _ := args["title"].(string)
	if msgType == "" {
		msgType = "text"
	}

	// Get token first
	tokenReq, _ := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/oauth2/accessToken", dingtalkAPI), nil)
	tokenReq.Header.Set("Content-Type", "application/json")
	tb, _ := json.Marshal(map[string]string{"appKey": appKey, "appSecret": appSecret})
	tokenReq.Body = io.NopCloser(bytes.NewReader(tb))
	tokenReq.ContentLength = int64(len(tb))

	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("auth: %v", err)}
	}
	var tr dingtalkTokenResp
	json.NewDecoder(tokenResp.Body).Decode(&tr)
	tokenResp.Body.Close()

	if tr.AccessToken == "" {
		return callToolResult{Error: "failed to get access token"}
	}

	// Build message
	msg := map[string]interface{}{}
	switch msgType {
	case "text":
		msg["msgtype"] = "text"
		msg["text"] = map[string]string{"content": content}
	case "markdown":
		msg["msgtype"] = "markdown"
		msg["markdown"] = map[string]string{"title": title, "text": content}
	case "link":
		msg["msgtype"] = "link"
		msg["link"] = map[string]string{
			"title":      title,
			"text":       content,
			"messageUrl": getStrArg(args, "messageUrl", ""),
			"picUrl":     getStrArg(args, "picUrl", ""),
		}
	default:
		msg["msgtype"] = "text"
		msg["text"] = map[string]string{"content": content}
	}

	mb, _ := json.Marshal(msg)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/robot/oToMessages/batchSend", dingtalkAPI), bytes.NewReader(mb))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", tr.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("send: %v", err)}
	}
	defer resp.Body.Close()

	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode == 200, Output: fmt.Sprintf("Message sent to %s.\nResponse: %s", userID, string(rbody))}
}

func dingtalkSendWebhook(ctx context.Context, args map[string]interface{}) callToolResult {
	webhookURL, _ := args["webhookUrl"].(string)
	secret, _ := args["secret"].(string)
	content, _ := args["content"].(string)
	msgType, _ := args["msgType"].(string)
	title, _ := args["title"].(string)
	if msgType == "" {
		msgType = "text"
	}

	// Add signature if secret is provided
	finalURL := webhookURL
	if secret != "" {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		sign := generateDingTalkSign(timestamp, secret)
		finalURL = fmt.Sprintf("%s&timestamp=%s&sign=%s", webhookURL, timestamp, url.QueryEscape(sign))
	}

	msg := map[string]interface{}{}
	switch msgType {
	case "text":
		msg["msgtype"] = "text"
		msg["text"] = map[string]string{"content": content}
	case "markdown":
		msg["msgtype"] = "markdown"
		msg["markdown"] = map[string]string{"title": title, "text": content}
	case "link":
		msg["msgtype"] = "link"
		msg["link"] = map[string]string{
			"title":      title,
			"text":       content,
			"messageUrl": getStrArg(args, "messageUrl", ""),
			"picUrl":     getStrArg(args, "picUrl", ""),
		}
	default:
		msg["msgtype"] = "text"
		msg["text"] = map[string]string{"content": content}
	}

	b, _ := json.Marshal(msg)
	resp, err := http.Post(finalURL, "application/json", bytes.NewReader(b))
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("webhook: %v", err)}
	}
	defer resp.Body.Close()

	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: fmt.Sprintf("Webhook sent. Status: %d\n%s", resp.StatusCode, string(rbody))}
}

// generateDingTalkSign computes the HMAC-SHA256 signature for DingTalk robot webhooks.
func generateDingTalkSign(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func getStrArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
