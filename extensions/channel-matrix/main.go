// Channel Matrix extension for Mneme.
//
// Provides Matrix messaging integration tools:
//   - matrix_send: send a message to a Matrix room
//   - matrix_join: join a Matrix room
//   - matrix_sync: sync recent messages from joined rooms
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
	"net/url"
	"os"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-matrix",
		Version:     "0.1.0",
		Description: "Matrix channel: send messages, join rooms, sync",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "matrix_send",
		Description: "Send a message to a Matrix room. Requires MATRIX_HOMESERVER, MATRIX_ACCESS_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"roomId": map[string]interface{}{"type": "string", "description": "Matrix room ID (e.g. !abc123:matrix.org)"},
				"body":   map[string]interface{}{"type": "string", "description": "Message body (plain text)"},
				"format": map[string]interface{}{"type": "string", "description": "Message format: plain, markdown (via formatted_body)"},
			},
			"required": []string{"roomId", "body"},
		},
		Permission: "execute",
		HasEffects: true,
	}, matrixSend)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "matrix_join",
		Description: "Join a Matrix room by ID or alias. Requires MATRIX_HOMESERVER, MATRIX_ACCESS_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"roomIdOrAlias": map[string]interface{}{"type": "string", "description": "Room ID (!xxx:server) or alias (#room:server)"},
			},
			"required": []string{"roomIdOrAlias"},
		},
		Permission: "execute",
		HasEffects: true,
	}, matrixJoin)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "matrix_sync",
		Description: "Sync recent messages from the Matrix homeserver. Requires MATRIX_HOMESERVER, MATRIX_ACCESS_TOKEN env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"since":  map[string]interface{}{"type": "string", "description": "Sync token from previous sync (optional)"},
				"filter": map[string]interface{}{"type": "string", "description": "Event filter JSON (optional)"},
				"limit":  map[string]interface{}{"type": "number", "description": "Max events per room (default 10)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, matrixSync)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-matrix: %v\n", err)
		os.Exit(1)
	}
}

func getMatrixConfig() (homeserver, token string, err error) {
	homeserver = os.Getenv("MATRIX_HOMESERVER")
	token = os.Getenv("MATRIX_ACCESS_TOKEN")
	if homeserver == "" || token == "" {
		return "", "", fmt.Errorf("MATRIX_HOMESERVER and MATRIX_ACCESS_TOKEN must be set")
	}
	return homeserver, token, nil
}

func matrixSend(ctx context.Context, args map[string]interface{}) extsdk.Result {
	hs, token, err := getMatrixConfig()
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	roomID, _ := args["roomId"].(string)
	body, _ := args["body"].(string)
	if roomID == "" || body == "" {
		return extsdk.Result{Error: "roomId and body are required"}
	}

	txnID := fmt.Sprintf("oh_%d", time.Now().UnixMilli())
	payload := map[string]interface{}{
		"msgtype": "m.text",
		"body":    body,
	}
	if format, _ := args["format"].(string); format == "markdown" {
		payload["format"] = "org.matrix.custom.html"
		payload["formatted_body"] = body
	}

	b, _ := json.Marshal(payload)
	reqURL := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		hs, url.PathEscape(roomID), url.PathEscape(txnID))
	req, _ := http.NewRequestWithContext(ctx, "PUT", reqURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("matrix send: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func matrixJoin(ctx context.Context, args map[string]interface{}) extsdk.Result {
	hs, token, err := getMatrixConfig()
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	roomID, _ := args["roomIdOrAlias"].(string)
	if roomID == "" {
		return extsdk.Result{Error: "roomIdOrAlias is required"}
	}

	reqURL := fmt.Sprintf("%s/_matrix/client/v3/join/%s", hs, url.PathEscape(roomID))
	req, _ := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("matrix join: %v", err)}
	}
	defer resp.Body.Close()
	rbody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(rbody)}
}

func matrixSync(ctx context.Context, args map[string]interface{}) extsdk.Result {
	hs, token, err := getMatrixConfig()
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	since, _ := args["since"].(string)
	filter, _ := args["filter"].(string)
	limit := 10
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}

	reqURL := fmt.Sprintf("%s/_matrix/client/v3/sync?timeout=5000", hs)
	if since != "" {
		reqURL += "&since=" + url.QueryEscape(since)
	}
	if filter != "" {
		reqURL += "&filter=" + url.QueryEscape(filter)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("matrix sync: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]struct {
				Timeline struct {
					Events []struct {
						Type    string `json:"type"`
						Sender  string `json:"sender"`
						Content struct {
							Body string `json:"body"`
						} `json:"content"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	json.Unmarshal(body, &data)

	var out string
	out += fmt.Sprintf("Sync token: %s\n\n", data.NextBatch)
	for roomID, room := range data.Rooms.Join {
		out += fmt.Sprintf("Room: %s\n", roomID)
		count := 0
		for _, ev := range room.Timeline.Events {
			if ev.Type == "m.room.message" && count < limit {
				out += fmt.Sprintf("  [%s] %s\n", ev.Sender, ev.Content.Body)
				count++
			}
		}
	}
	return extsdk.Result{Success: true, Output: out}
}

func getInt(args map[string]interface{}, key string) (int, bool) {
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
