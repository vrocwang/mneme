// Gmail Unsubscribe extension for Mneme.
//
// Provides:
//   - gmail_unsubscribe: find and unsubscribe from mailing lists in Gmail
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "tool-gmail-unsubscribe",
		Version:     "0.1.0",
		Description: "Find and unsubscribe from mailing lists in Gmail via the Gmail API",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, gmailUnsubscribe)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tool-gmail-unsubscribe: %v\n", err)
		os.Exit(1)
	}
}

func gmailUnsubscribe(ctx context.Context, args map[string]interface{}) extsdk.Result {
	email, _ := args["email"].(string)
	if email == "" {
		return extsdk.Result{Error: "email is required"}
	}

	token, _ := args["token"].(string)
	if token == "" {
		token = os.Getenv("GMAIL_TOKEN")
	}
	if token == "" {
		return extsdk.Result{Error: "token is required (or set GMAIL_TOKEN env var)"}
	}

	// Query Gmail API for recent messages (last 50)
	gmailURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/%s/messages?maxResults=50&q=newer_than:14d", email)

	req, err := http.NewRequestWithContext(ctx, "GET", gmailURL, nil)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("gmail API: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return extsdk.Result{Error: "Gmail API authentication failed — check your token"}
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return extsdk.Result{Error: fmt.Sprintf("Gmail API error %d: %s", resp.StatusCode, truncate(string(body), 300))}
	}

	var listResp struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse: %v", err)}
	}

	if len(listResp.Messages) == 0 {
		return extsdk.Result{Success: true, Output: "No recent messages found."}
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
		return extsdk.Result{Success: true, Output: "No List-Unsubscribe headers found in recent messages."}
	}

	b, _ := json.MarshalIndent(results, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
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
