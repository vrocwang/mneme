// Gmail Unsubscribe extension for Mneme.
//
// Provides:
//   - gmail_unsubscribe: find and unsubscribe from mailing lists in Gmail
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
	Name:        "tool-gmail-unsubscribe",
	Version:     "0.1.0",
	Description: "Find and unsubscribe from mailing lists in Gmail via the Gmail API",
	Tools:       []string{"gmail_unsubscribe"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "gmail_unsubscribe",
		Description: "Scan recent Gmail emails for List-Unsubscribe headers and attempt to unsubscribe from mailing lists",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"email": map[string]interface{}{"type": "string", "description": "Gmail email address to scan"},
				"token": map[string]interface{}{"type": "string", "description": "Gmail API access token (optional, falls back to GMAIL_TOKEN env var)"},
			},
			"required": []string{"email"},
		},
		Permission: "execute",
		HasEffects: true,
	},
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("tool-gmail-unsubscribe extension starting")
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
		type lr struct{ Tools []toolDef }
		result, _ := json.Marshal(lr{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "gmail_unsubscribe":
			result = gmailUnsubscribe(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func gmailUnsubscribe(ctx context.Context, args map[string]interface{}) callToolResult {
	email, _ := args["email"].(string)
	if email == "" {
		return callToolResult{Error: "email is required"}
	}

	token, _ := args["token"].(string)
	if token == "" {
		token = os.Getenv("GMAIL_TOKEN")
	}
	if token == "" {
		return callToolResult{Error: "token is required (or set GMAIL_TOKEN env var)"}
	}

	// Query Gmail API for recent messages (last 50)
	gmailURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/%s/messages?maxResults=50&q=newer_than:14d", email)

	req, err := http.NewRequestWithContext(ctx, "GET", gmailURL, nil)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("gmail API: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return callToolResult{Error: "Gmail API authentication failed — check your token"}
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return callToolResult{Error: fmt.Sprintf("Gmail API error %d: %s", resp.StatusCode, truncate(string(body), 300))}
	}

	var listResp struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse: %v", err)}
	}

	if len(listResp.Messages) == 0 {
		return callToolResult{Success: true, Output: "No recent messages found."}
	}

	// For each message, fetch headers and look for List-Unsubscribe
	type unsubscribeResult struct {
		MessageID    string   `json:"message_id"`
		Subject      string   `json:"subject"`
		From         string   `json:"from"`
		UnsubMethods []string `json:"unsubscribe_methods"`
		Status       string   `json:"status"`
	}

	var results []unsubscribeResult
	for i, msg := range listResp.Messages {
		if i >= 20 {
			break // limit to 20 for performance
		}

		detailURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/%s/messages/%s?format=metadata&metadataHeaders=Subject&metadataHeaders=From&metadataHeaders=List-Unsubscribe", email, msg.ID)

		dReq, dReqErr := http.NewRequestWithContext(ctx, "GET", detailURL, nil)
		if dReqErr != nil {
			continue
		}
		dReq.Header.Set("Authorization", "Bearer "+token)
		dResp, dErr := httpClient.Do(dReq)
		if dErr != nil {
			continue
		}
		dBody, _ := io.ReadAll(dResp.Body)
		dResp.Body.Close()

		var detail struct {
			Payload struct {
				Headers []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(dBody, &detail); err != nil {
			continue
		}

		var subject, from, unsubHeader string
		for _, h := range detail.Payload.Headers {
			switch h.Name {
			case "Subject":
				subject = h.Value
			case "From":
				from = h.Value
			case "List-Unsubscribe":
				unsubHeader = h.Value
			}
		}

		if unsubHeader != "" {
			ur := unsubscribeResult{
				MessageID:    msg.ID,
				Subject:      subject,
				From:         from,
				UnsubMethods: parseUnsubscribeHeader(unsubHeader),
				Status:       "found",
			}

			// Attempt unsubscribe via HTTP if a URL is available
			for _, method := range ur.UnsubMethods {
				if strings.HasPrefix(method, "http") {
					unsubReq, _ := http.NewRequestWithContext(ctx, "GET", method, nil)
					unsubResp, uErr := httpClient.Do(unsubReq)
					if uErr != nil {
						ur.Status = fmt.Sprintf("unsub failed: %v", uErr)
					} else {
						unsubResp.Body.Close()
						ur.Status = fmt.Sprintf("unsubscribed (HTTP %d)", unsubResp.StatusCode)
					}
					break
				}
				if strings.HasPrefix(method, "mailto") {
					ur.Status = "mailto link found (manual unsubscribe required): " + method
				}
			}
			results = append(results, ur)
		}
	}

	if len(results) == 0 {
		return callToolResult{Success: true, Output: "No List-Unsubscribe headers found in recent messages."}
	}

	b, _ := json.MarshalIndent(results, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func parseUnsubscribeHeader(header string) []string {
	// Headers are typically <url>, <mailto:...> or comma-separated
	var methods []string
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "<>")
		if part != "" {
			methods = append(methods, part)
		}
	}
	return methods
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}
