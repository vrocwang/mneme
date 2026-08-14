// Channel Mattermost extension for Mneme.
//
// Provides Mattermost integration tools:
//   - mattermost_send: send message to a channel
//   - mattermost_webhook: send via incoming webhook
//   - mattermost_list_channels: list accessible channels
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

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-mattermost",
		Version:     "0.1.0",
		Description: "Mattermost channel: send messages, webhooks, list channels",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, mattermostSend)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, mattermostWebhook)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, mattermostListChannels)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-mattermost: %v\n", err)
		os.Exit(1)
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

func mattermostSend(ctx context.Context, args map[string]interface{}) extsdk.Result {
	baseURL, token, err := getMattermostConfig()
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}

	channelID, _ := args["channelId"].(string)
	message, _ := args["message"].(string)
	if channelID == "" || message == "" {
		return extsdk.Result{Error: "channelId and message are required"}
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
		return extsdk.Result{Error: fmt.Sprintf("mattermost: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func mattermostWebhook(ctx context.Context, args map[string]interface{}) extsdk.Result {
	webhookURL, _ := args["webhookUrl"].(string)
	text, _ := args["text"].(string)
	if webhookURL == "" || text == "" {
		return extsdk.Result{Error: "webhookUrl and text are required"}
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
		return extsdk.Result{Error: fmt.Sprintf("webhook: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func mattermostListChannels(ctx context.Context, args map[string]interface{}) extsdk.Result {
	baseURL, token, err := getMattermostConfig()
	if err != nil {
		return extsdk.Result{Error: err.Error()}
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
		return extsdk.Result{Error: fmt.Sprintf("list channels: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(rbody)}
}
