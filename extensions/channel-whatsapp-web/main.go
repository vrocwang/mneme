// Channel WhatsApp Web extension for Mneme.
//
// Provides WhatsApp Web integration tools via the WhatsApp Business API or
// a bridge service. For full WhatsApp access, use the WhatsApp Business API
// or a library like whatsmeow.
//
// Tools:
//   - whatsapp_web_send: send a message via WhatsApp Business API
//   - whatsapp_web_status: check WhatsApp API connection status
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
		Name:        "channel-whatsapp-web",
		Version:     "0.1.0",
		Description: "WhatsApp Web channel: send messages via WhatsApp Business API",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "whatsapp_web_send",
		Description: "Send a WhatsApp message via the WhatsApp Business API. Requires WHATSAPP_API_TOKEN and WHATSAPP_PHONE_ID env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"to":   map[string]interface{}{"type": "string", "description": "Recipient phone number with country code (e.g. 15551234567)"},
				"body": map[string]interface{}{"type": "string", "description": "Message text (max 4096 chars)"},
			},
			"required": []string{"to", "body"},
		},
		Permission: "execute",
		HasEffects: true,
	}, whatsappSend)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "whatsapp_web_status",
		Description: "Check WhatsApp Business API connection status",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	}, whatsappStatus)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-whatsapp-web: %v\n", err)
		os.Exit(1)
	}
}

func getWACreds() (token, phoneID string, err error) {
	token = os.Getenv("WHATSAPP_API_TOKEN")
	phoneID = os.Getenv("WHATSAPP_PHONE_ID")
	if token == "" || phoneID == "" {
		return "", "", fmt.Errorf("WHATSAPP_API_TOKEN and WHATSAPP_PHONE_ID must be set")
	}
	return token, phoneID, nil
}

func whatsappSend(ctx context.Context, args map[string]interface{}) extsdk.Result {
	token, phoneID, err := getWACreds()
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}

	to, _ := args["to"].(string)
	body, _ := args["body"].(string)
	if to == "" || body == "" {
		return extsdk.Result{Error: "to and body are required"}
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": body},
	}
	b, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", phoneID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("whatsapp send: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func whatsappStatus(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = args
	token, phoneID, err := getWACreds()
	if err != nil {
		return extsdk.Result{Success: false, Error: fmt.Sprintf("WhatsApp API not configured: %v", err)}
	}

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s", phoneID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("status check: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 400 {
		return extsdk.Result{Success: true, Output: fmt.Sprintf("WhatsApp API connected\nPhone ID: %s\n%s", phoneID, string(rbody))}
	}
	return extsdk.Result{Error: fmt.Sprintf("API check failed (status %d): %s", resp.StatusCode, string(rbody))}
}
