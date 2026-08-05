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
	Name:        "channel-whatsapp-web",
	Version:     "0.1.0",
	Description: "WhatsApp Web channel: send messages via WhatsApp Business API",
	Tools:       []string{"whatsapp_web_send", "whatsapp_web_status"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
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
	},
	{
		Name:        "whatsapp_web_status",
		Description: "Check WhatsApp Business API connection status",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	},
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("channel-whatsapp-web extension starting")
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
		case "whatsapp_web_send":
			result = whatsappSend(ctx, params.Args)
		case "whatsapp_web_status":
			result = whatsappStatus(ctx)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
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

func whatsappSend(ctx context.Context, args map[string]interface{}) callToolResult {
	token, phoneID, err := getWACreds()
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	to, _ := args["to"].(string)
	body, _ := args["body"].(string)
	if to == "" || body == "" {
		return callToolResult{Error: "to and body are required"}
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
		return callToolResult{Error: fmt.Sprintf("whatsapp send: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func whatsappStatus(ctx context.Context) callToolResult {
	token, phoneID, err := getWACreds()
	if err != nil {
		return callToolResult{Success: false, Error: fmt.Sprintf("WhatsApp API not configured: %v", err)}
	}

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s", phoneID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("status check: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 400 {
		return callToolResult{Success: true, Output: fmt.Sprintf("WhatsApp API connected\nPhone ID: %s\n%s", phoneID, string(rbody))}
	}
	return callToolResult{Error: fmt.Sprintf("API check failed (status %d): %s", resp.StatusCode, string(rbody))}
}
