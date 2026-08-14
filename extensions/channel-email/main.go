// Channel Email extension for Mneme.
//
// Provides email integration tools via SMTP and IMAP:
//   - email_send: send email via SMTP
//   - email_check: check inbox via IMAP
//   - email_search: search emails
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-email",
		Version:     "0.1.0",
		Description: "Email channel integration: send, check, and search emails via SMTP/IMAP",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, sendEmail)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, checkEmail)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, searchEmail)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-email: %v\n", err)
		os.Exit(1)
	}
}

// ── Tool implementations ──────────────────────────────────────────

func sendEmail(ctx context.Context, args map[string]interface{}) extsdk.Result {
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
		return extsdk.Result{Error: fmt.Sprintf("connect to SMTP: %v", err)}
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("SMTP client: %v", err)}
	}
	defer client.Quit()

	if port == 587 {
		if err := client.StartTLS(&tls.Config{ServerName: smtpHost}); err != nil {
			return extsdk.Result{Error: fmt.Sprintf("STARTTLS: %v", err)}
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return extsdk.Result{Error: fmt.Sprintf("SMTP auth: %v", err)}
		}
	}

	if err := client.Mail(from); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("MAIL FROM: %v", err)}
	}
	if err := client.Rcpt(to); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("RCPT TO: %v", err)}
	}

	w, err := client.Data()
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("DATA: %v", err)}
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("write: %v", err)}
	}
	w.Close()

	return extsdk.Result{Success: true, Output: fmt.Sprintf("Email sent to %s (subject: %s)", to, subject)}
}

func checkEmail(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	// IMAP checking requires a full IMAP client library.
	return extsdk.Result{Error: "IMAP check not implemented: full IMAP support requires github.com/emersion/go-imap dependency"}
}

func searchEmail(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	return extsdk.Result{Error: "IMAP search not implemented: full IMAP support requires github.com/emersion/go-imap dependency"}
}

// ── Helpers ──────────────────────────────────────────────────────

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
