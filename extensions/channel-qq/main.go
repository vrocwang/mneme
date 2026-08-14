// Channel QQ extension for Mneme.
//
// Provides QQ (Tencent) messaging integration tools via QQ Bot API:
//   - qq_send_message: send a message to a QQ channel/group
//   - qq_status: check QQ Bot API connection status
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
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-qq",
		Version:     "0.1.0",
		Description: "QQ channel: send messages via QQ Bot API",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, qqSendMsg)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "qq_status",
		Description: "Check QQ Bot API connection status",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	}, qqStatus)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-qq: %v\n", err)
		os.Exit(1)
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

func qqSendMsg(ctx context.Context, args map[string]interface{}) extsdk.Result {
	appID, botToken, err := getQQCreds()
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}

	channelID, _ := args["channelId"].(string)
	content, _ := args["content"].(string)
	msgType, _ := args["msgType"].(string)
	if msgType == "" {
		msgType = "text"
	}
	if channelID == "" || content == "" {
		return extsdk.Result{Error: "channelId and content are required"}
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
		return extsdk.Result{Error: fmt.Sprintf("qq send: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func qqStatus(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = args
	appID, _, err := getQQCreds()
	if err != nil {
		return extsdk.Result{Success: false, Error: fmt.Sprintf("QQ Bot API not configured: %v", err)}
	}

	url := fmt.Sprintf("https://api.sgroup.qq.com/users/@me")
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bot %s.%s", appID, os.Getenv("QQ_BOT_TOKEN")))
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("status: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 400 {
		return extsdk.Result{Success: true, Output: fmt.Sprintf("QQ Bot API connected\nApp ID: %s\n%s", appID, string(rbody))}
	}
	return extsdk.Result{Error: fmt.Sprintf("API check failed (status %d): %s", resp.StatusCode, string(rbody))}
}
