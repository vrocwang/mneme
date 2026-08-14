// GitBooks extension for Mneme.
//
// Provides:
//   - gitbooks_search: search GitBooks documentation via the GitBook API
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
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

var httpClient = &http.Client{Timeout: 15 * time.Second}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "tool-gitbooks",
		Version:     "0.1.0",
		Description: "Search GitBooks documentation via the GitBook API",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "gitbooks_search",
		Description: "Search GitBooks documentation via the GitBook API",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":    map[string]interface{}{"type": "string", "description": "Search query string"},
				"space_id": map[string]interface{}{"type": "string", "description": "GitBook space ID (optional, falls back to GITBOOK_SPACE_ID env var)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, gitbooksSearch)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tool-gitbooks: %v\n", err)
		os.Exit(1)
	}
}

func gitbooksSearch(ctx context.Context, args map[string]interface{}) extsdk.Result {
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
	}

	spaceID, _ := args["space_id"].(string)
	if spaceID == "" {
		spaceID = os.Getenv("GITBOOK_SPACE_ID")
	}
	if spaceID == "" {
		return extsdk.Result{Error: "space_id is required (or set GITBOOK_SPACE_ID env var)"}
	}

	apiURL := fmt.Sprintf("https://api.gitbook.com/v1/spaces/%s/search?query=%s",
		url.PathEscape(spaceID), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("request: %v", err)}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("search: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("read response: %v", err)}
	}
	if resp.StatusCode >= 400 {
		return extsdk.Result{Error: fmt.Sprintf("API error %d: %s", resp.StatusCode, truncate(string(body), 500))}
	}

	var pretty interface{}
	if err := json.Unmarshal(body, &pretty); err != nil {
		return extsdk.Result{Success: true, Output: truncate(string(body), 4000)}
	}
	b, _ := json.MarshalIndent(pretty, "", "  ")
	return extsdk.Result{Success: true, Output: truncate(string(b), 4000)}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}
