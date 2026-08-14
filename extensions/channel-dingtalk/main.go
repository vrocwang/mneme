// Channel DingTalk extension for Mneme.
//
// Provides DingTalk messaging integration tools:
//   - dingtalk_send_message: send text/markdown message to a conversation
//   - dingtalk_send_webhook: send message via webhook robot URL
//   - dingtalk_get_token: get access token for API calls
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-dingtalk",
		Version:     "0.1.0",
		Description: "DingTalk channel: send messages, webhooks, access token management",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, dingtalkSendMsg)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, dingtalkSendWebhook)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, dingtalkGetToken)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-dingtalk: %v\n", err)
		os.Exit(1)
	}
}

// ── DingTalk API ──────────────────────────────────────────────────

const dingtalkAPI = "https://api.dingtalk.com/v1.0"

type dingtalkTokenResp struct {
	AccessToken string `json:"accessToken"`
	ExpireIn    int64  `json:"expireIn"`
}

func dingtalkGetToken(ctx context.Context, args map[string]interface{}) extsdk.Result {
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
		return extsdk.Result{Error: fmt.Sprintf("token request: %v", err)}
	}
	defer resp.Body.Close()

	rbody, _ := io.ReadAll(resp.Body)
	var tr dingtalkTokenResp
	json.Unmarshal(rbody, &tr)
	if tr.AccessToken == "" {
		return extsdk.Result{Error: fmt.Sprintf("failed to get token (status %d): %s", resp.StatusCode, string(rbody))}
	}
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Access token: %s... (expires in %ds)", tr.AccessToken[:min(20, len(tr.AccessToken))], tr.ExpireIn)}
}

func dingtalkSendMsg(ctx context.Context, args map[string]interface{}) extsdk.Result {
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
		return extsdk.Result{Error: fmt.Sprintf("auth: %v", err)}
	}
	var tr dingtalkTokenResp
	json.NewDecoder(tokenResp.Body).Decode(&tr)
	tokenResp.Body.Close()

	if tr.AccessToken == "" {
		return extsdk.Result{Error: "failed to get access token"}
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
		return extsdk.Result{Error: fmt.Sprintf("send: %v", err)}
	}
	defer resp.Body.Close()

	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode == 200, Output: fmt.Sprintf("Message sent to %s.\nResponse: %s", userID, string(rbody))}
}

func dingtalkSendWebhook(ctx context.Context, args map[string]interface{}) extsdk.Result {
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
		return extsdk.Result{Error: fmt.Sprintf("webhook: %v", err)}
	}
	defer resp.Body.Close()

	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: fmt.Sprintf("Webhook sent. Status: %d\n%s", resp.StatusCode, string(rbody))}
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
