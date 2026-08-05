// Polymarket extension for Mneme.
//
// Provides:
//   - polymarket_markets: query Polymarket prediction market data
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
	"net/url"
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
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

var extManifest = manifest{
	Name:        "tool-polymarket",
	Version:     "0.1.0",
	Description: "Query Polymarket prediction market data via the Gamma API",
	Tools:       []string{"polymarket_markets"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
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
	},
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("tool-polymarket extension starting")
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
		case "polymarket_markets":
			result = polymarketMarkets(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func polymarketMarkets(ctx context.Context, args map[string]interface{}) callToolResult {
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
		return callToolResult{Error: fmt.Sprintf("request: %v", err)}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("API: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return callToolResult{Error: fmt.Sprintf("API error %d: %s", resp.StatusCode, truncate(string(body), 500))}
	}

	// Parse and format the response
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return callToolResult{Success: true, Output: truncate(string(body), 4000)}
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
		return callToolResult{Success: true, Output: string(b)}
	}

	b, _ := json.MarshalIndent(raw, "", "  ")
	return callToolResult{Success: true, Output: truncate(string(b), 4000)}
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
