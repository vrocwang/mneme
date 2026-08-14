// Polymarket extension for Mneme.
//
// Provides:
//   - polymarket_markets: query Polymarket prediction market data
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
		Name:        "tool-polymarket",
		Version:     "0.1.0",
		Description: "Query Polymarket prediction market data via the Gamma API",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "polymarket_markets",
		Description: "Query Polymarket prediction markets. Search by tag or get all active markets.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "Market tag to filter by (e.g. politics, crypto, sports). Leave empty for all active markets."},
				"limit": map[string]interface{}{"type": "number", "description": "Max results to return (default 20)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, polymarketMarkets)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tool-polymarket: %v\n", err)
		os.Exit(1)
	}
}

func polymarketMarkets(ctx context.Context, args map[string]interface{}) extsdk.Result {
	limit := 20
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}

	var apiURL string
	query, _ := args["query"].(string)
	if query != "" {
		apiURL = fmt.Sprintf("https://gamma-api.polymarket.com/markets?tag=%s&limit=%d",
			url.QueryEscape(query), limit)
	} else {
		apiURL = fmt.Sprintf("https://gamma-api.polymarket.com/markets?limit=%d", limit)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("request: %v", err)}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("API: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return extsdk.Result{Error: fmt.Sprintf("API error %d: %s", resp.StatusCode, truncate(string(body), 500))}
	}

	// Parse and format the response
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return extsdk.Result{Success: true, Output: truncate(string(body), 4000)}
	}

	// Try to extract market summaries
	var markets []interface{}
	if err := json.Unmarshal(body, &markets); err == nil {
		type marketSummary struct {
			Title      string   `json:"question"`
			Slug       string   `json:"slug"`
			Volume     float64  `json:"volume"`
			OutcomePRs []string `json:"outcome_prices,omitempty"`
		}
		var summaries []marketSummary
		for _, m := range markets {
			mm, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			ms := marketSummary{}
			if t, ok := mm["question"].(string); ok {
				ms.Title = t
			}
			if s, ok := mm["slug"].(string); ok {
				ms.Slug = s
			}
			if v, ok := mm["volume"].(float64); ok {
				ms.Volume = v
			}
			// Extract outcome prices from token pairs
			if tokens, ok := mm["tokens"].([]interface{}); ok {
				for _, t := range tokens {
					if token, ok := t.(map[string]interface{}); ok {
						if outcome, ok := token["outcome"].(string); ok {
							if price, ok := token["price"].(float64); ok {
								ms.OutcomePRs = append(ms.OutcomePRs, fmt.Sprintf("%s: %.4f", outcome, price))
							}
						}
					}
				}
			}
			summaries = append(summaries, ms)
			if len(summaries) >= limit {
				break
			}
		}
		b, _ := json.MarshalIndent(summaries, "", "  ")
		return extsdk.Result{Success: true, Output: string(b)}
	}

	b, _ := json.MarshalIndent(raw, "", "  ")
	return extsdk.Result{Success: true, Output: truncate(string(b), 4000)}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
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
