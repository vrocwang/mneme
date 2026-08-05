// Channel Email extension for Mneme.
//
// Provides email integration tools via SMTP and IMAP:
//   - email_send: send email via SMTP
//   - email_check: check inbox via IMAP
//   - email_search: search emails
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"os"
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
	Name:        "channel-email",
	Version:     "0.1.0",
	Description: "Email channel integration: send, check, and search emails via SMTP/IMAP",
	Tools:       []string{"email_send", "email_check", "email_search"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "email_send",
		Description: "Send an email via SMTP. Requires SMTP configuration (host, port, username, password).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"to":       map[string]interface{}{"type": "string", "description": "Recipient email address"},
				"subject":  map[string]interface{}{"type": "string", "description": "Email subject"},
				"body":     map[string]interface{}{"type": "string", "description": "Email body (plain text)"},
				"smtpHost": map[string]interface{}{"type": "string", "description": "SMTP server host (e.g. smtp.gmail.com)"},
				"smtpPort": map[string]interface{}{"type": "number", "description": "SMTP port (default 587)"},
				"username": map[string]interface{}{"type": "string", "description": "SMTP username/email"},
				"password": map[string]interface{}{"type": "string", "description": "SMTP password or app password"},
			},
			"required": []string{"to", "subject", "body", "smtpHost", "username", "password"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "email_check",
		Description: "Check recent emails in the inbox via IMAP",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"imapHost": map[string]interface{}{"type": "string", "description": "IMAP server host (e.g. imap.gmail.com)"},
				"imapPort": map[string]interface{}{"type": "number", "description": "IMAP port (default 993)"},
				"username": map[string]interface{}{"type": "string", "description": "IMAP username/email"},
				"password": map[string]interface{}{"type": "string", "description": "IMAP password or app password"},
				"limit":    map[string]interface{}{"type": "number", "description": "Max emails to return (default 10)"},
			},
			"required": []string{"imapHost", "username", "password"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "email_search",
		Description: "Search emails by subject, sender, or date via IMAP",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"imapHost": map[string]interface{}{"type": "string", "description": "IMAP server host"},
				"imapPort": map[string]interface{}{"type": "number", "description": "IMAP port (default 993)"},
				"username": map[string]interface{}{"type": "string", "description": "IMAP username/email"},
				"password": map[string]interface{}{"type": "string", "description": "IMAP password or app password"},
				"query":    map[string]interface{}{"type": "string", "description": "Search query: subject keywords, FROM address, SINCE date"},
				"limit":    map[string]interface{}{"type": "number", "description": "Max results (default 20)"},
			},
			"required": []string{"imapHost", "username", "password", "query"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

// ── Main ──────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("channel-email extension starting", "version", extManifest.Version)

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
		case "email_send":
			result = sendEmail(ctx, params.Args)
		case "email_check":
			result = checkEmail(ctx, params.Args)
		case "email_search":
			result = searchEmail(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

// ── Tool implementations ──────────────────────────────────────────

func sendEmail(ctx context.Context, args map[string]interface{}) callToolResult {
	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	smtpHost, _ := args["smtpHost"].(string)
	username, _ := args["username"].(string)
	password, _ := args["password"].(string)

	port := 587
	if p, ok := intFromArgs(args, "smtpPort"); ok && p > 0 {
		port = p
	}

	from := username
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)

	addr := fmt.Sprintf("%s:%d", smtpHost, port)
	var auth smtp.Auth
	if password != "" {
		auth = smtp.PlainAuth("", username, password, smtpHost)
	}

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("connect to SMTP: %v", err)}
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("SMTP client: %v", err)}
	}
	defer client.Quit()

	if port == 587 {
		if err := client.StartTLS(&tls.Config{ServerName: smtpHost}); err != nil {
			return callToolResult{Error: fmt.Sprintf("STARTTLS: %v", err)}
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return callToolResult{Error: fmt.Sprintf("SMTP auth: %v", err)}
		}
	}

	if err := client.Mail(from); err != nil {
		return callToolResult{Error: fmt.Sprintf("MAIL FROM: %v", err)}
	}
	if err := client.Rcpt(to); err != nil {
		return callToolResult{Error: fmt.Sprintf("RCPT TO: %v", err)}
	}

	w, err := client.Data()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("DATA: %v", err)}
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("write: %v", err)}
	}
	w.Close()

	return callToolResult{Success: true, Output: fmt.Sprintf("Email sent to %s (subject: %s)", to, subject)}
}

func checkEmail(ctx context.Context, args map[string]interface{}) callToolResult {
	imapHost, _ := args["imapHost"].(string)
	username, _ := args["username"].(string)
	password, _ := args["password"].(string)

	limit := 10
	if l, ok := intFromArgs(args, "limit"); ok && l > 0 {
		limit = l
	}

	port := 993
	if p, ok := intFromArgs(args, "imapPort"); ok && p > 0 {
		port = p
	}

	_ = imapHost
	_ = username
	_ = password
	_ = port
	_ = limit

	// IMAP checking requires a full IMAP client library.
	return callToolResult{Success: false, Error: fmt.Sprintf(
		"IMAP check not implemented: full IMAP support requires github.com/emersion/go-imap dependency",
	)}
}

func searchEmail(ctx context.Context, args map[string]interface{}) callToolResult {
	imapHost, _ := args["imapHost"].(string)
	username, _ := args["username"].(string)
	password, _ := args["password"].(string)
	query, _ := args["query"].(string)

	limit := 20
	if l, ok := intFromArgs(args, "limit"); ok && l > 0 {
		limit = l
	}

	_ = imapHost
	_ = username
	_ = password
	_ = query
	_ = limit

	return callToolResult{Success: false, Error: fmt.Sprintf(
		"IMAP search not implemented: full IMAP support requires github.com/emersion/go-imap dependency",
	)}
}

// ── Helpers ────────────────────────────────────────────────────────

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
	case int64:
		return int(n), true
	}
	return 0, false
}
