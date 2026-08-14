// Channel Lark (Feishu) extension for Mneme.
//
// Provides Lark/Feishu messaging integration tools:
//   - lark_send_message: send a text or card message to a chat
//   - lark_send_webhook: send a message via webhook URL
//   - lark_list_groups: list accessible groups/chats
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-lark",
		Version:     "0.1.0",
		Description: "Lark/Feishu channel: send messages, webhooks, list groups",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "lark_send_message",
		Description: "Send a text or interactive card message to a Lark/Feishu chat via the Open API. Requires app credentials (app_id, app_secret).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"appId":     map[string]interface{}{"type": "string", "description": "Lark app ID"},
				"appSecret": map[string]interface{}{"type": "string", "description": "Lark app secret"},
				"chatId":    map[string]interface{}{"type": "string", "description": "Target chat ID (e.g. oc_xxx)"},
				"msgType":   map[string]interface{}{"type": "string", "description": "Message type: text, interactive, post (default: text)"},
				"content":   map[string]interface{}{"type": "string", "description": "Message content (text string or card JSON)"},
			},
			"required": []string{"appId", "appSecret", "content"},
		},
		Permission: "execute",
		HasEffects: true,
	}, larkSendMsg)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "lark_send_webhook",
		Description: "Send a message to a Lark/Feishu group via an incoming webhook URL (simpler setup — no app credentials needed).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"webhookUrl": map[string]interface{}{"type": "string", "description": "Incoming webhook URL from Lark bot settings"},
				"title":      map[string]interface{}{"type": "string", "description": "Message title (for rich format)"},
				"content":    map[string]interface{}{"type": "string", "description": "Message content"},
			},
			"required": []string{"webhookUrl", "content"},
		},
		Permission: "execute",
		HasEffects: true,
	}, larkSendWebhook)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "lark_list_groups",
		Description: "List Lark/Feishu chats/groups accessible to the app",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"appId":     map[string]interface{}{"type": "string", "description": "Lark app ID"},
				"appSecret": map[string]interface{}{"type": "string", "description": "Lark app secret"},
				"pageSize":  map[string]interface{}{"type": "number", "description": "Results per page (default 20)"},
			},
			"required": []string{"appId", "appSecret"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, larkListGroups)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-lark: %v\n", err)
		os.Exit(1)
	}
}

// ── Lark API ──────────────────────────────────────────────────────

const larkAPIBase = "https://open.feishu.cn/open-apis"

type larkTokenResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	} `json:"data,omitempty"`
	AppAccessToken       string `json:"app_access_token,omitempty"`
	TenantAccessTokenAlt string `json:"tenant_access_token,omitempty"`
}

func getLarkToken(ctx context.Context, appID, appSecret string) (string, error) {
	body := map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	}
	b, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", larkAPIBase+"/auth/v3/tenant_access_token/internal", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	var tr larkTokenResp
	json.NewDecoder(resp.Body).Decode(&tr)
	if tr.Code != 0 {
		return "", fmt.Errorf("lark auth failed: %s (code %d)", tr.Msg, tr.Code)
	}
	if tr.TenantAccessTokenAlt != "" {
		return tr.TenantAccessTokenAlt, nil
	}
	if tr.Data.TenantAccessToken != "" {
		return tr.Data.TenantAccessToken, nil
	}
	return "", fmt.Errorf("lark auth response missing token (code %d: %s)", tr.Code, tr.Msg)
}

func larkSendMsg(ctx context.Context, args map[string]interface{}) extsdk.Result {
	appID, _ := args["appId"].(string)
	appSecret, _ := args["appSecret"].(string)
	content, _ := args["content"].(string)
	chatID, _ := args["chatId"].(string)
	msgType, _ := args["msgType"].(string)
	if msgType == "" {
		msgType = "text"
	}

	token, err := getLarkToken(ctx, appID, appSecret)
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}

	msgContent := map[string]interface{}{}
	switch msgType {
	case "text":
		msgContent["text"] = content
	case "interactive":
		var card map[string]interface{}
		if err := json.Unmarshal([]byte(content), &card); err != nil {
			return extsdk.Result{Error: fmt.Sprintf("invalid card JSON: %v", err)}
		}
		msgContent = card
	default:
		msgContent["text"] = content
	}

	payload := map[string]interface{}{
		"msg_type": msgType,
		"content":  msgContent,
	}

	// If chatId is provided, send to specific chat; otherwise send as direct message
	// to the first available chat (requires receive_id)
	if chatID != "" {
		payload["receive_id"] = chatID
	}

	b, _ := json.Marshal(payload)
	endpoint := larkAPIBase + "/im/v1/messages?receive_id_type=chat_id"

	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("send message: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Message sent to Lark.\nResponse: %s", string(body))}
}

func larkSendWebhook(ctx context.Context, args map[string]interface{}) extsdk.Result {
	webhookURL, _ := args["webhookUrl"].(string)
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)

	payload := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title": map[string]interface{}{
					"tag":     "plain_text",
					"content": title,
				},
			},
			"elements": []map[string]interface{}{
				{
					"tag": "div",
					"text": map[string]interface{}{
						"tag":     "lark_md",
						"content": content,
					},
				},
			},
		},
	}

	b, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(b))
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("webhook: %v", err)}
	}
	defer resp.Body.Close()

	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: fmt.Sprintf("Webhook sent. Status: %d\n%s", resp.StatusCode, string(rbody))}
}

func larkListGroups(ctx context.Context, args map[string]interface{}) extsdk.Result {
	appID, _ := args["appId"].(string)
	appSecret, _ := args["appSecret"].(string)

	token, err := getLarkToken(ctx, appID, appSecret)
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}

	pageSize := 20
	if ps, ok := intFromArgs(args, "pageSize"); ok && ps > 0 {
		pageSize = ps
	}

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/im/v1/chats?page_size=%d", larkAPIBase, pageSize), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("list groups: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: true, Output: string(body)}
}

func intFromArgs(args map[string]interface{}, key string) (int, bool) {
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
