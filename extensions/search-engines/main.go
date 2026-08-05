// Search Engines extension for Mneme.
//
// Provides additional search backends beyond the core web_search:
//   - search_searxng: search via a self-hosted SearXNG instance
//   - search_querit: search via Querit.ai semantic API
//   - search_seltz: search via Seltz engine
//   - search_parallel: search across multiple engines concurrently
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
	"net/url"
	"os"
	"strings"
	"sync"
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
	Name:        "search-engines",
	Version:     "0.1.0",
	Description: "Additional search backends: SearXNG, Querit, Seltz, Parallel",
	Tools:       []string{"search_searxng", "search_querit", "search_seltz", "search_parallel"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "search_searxng",
		Description: "Search via a self-hosted SearXNG instance (privacy-respecting metasearch). Requires SEARXNG_URL env var.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":      map[string]interface{}{"type": "string", "description": "Search query"},
				"categories": map[string]interface{}{"type": "string", "description": "Comma-separated categories: general, news, images, videos, science, etc."},
				"maxResults": map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "search_querit",
		Description: "Search via Querit.ai semantic search API. Requires QUERIT_API_KEY env var.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":      map[string]interface{}{"type": "string", "description": "Search query (natural language)"},
				"maxResults": map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "search_seltz",
		Description: "Search via Seltz search engine. Requires SELTZ_API_KEY env var.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":      map[string]interface{}{"type": "string", "description": "Search query"},
				"maxResults": map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "search_parallel",
		Description: "Search across multiple engines in parallel and merge results. Uses SearXNG, Querit, and DuckDuckGo.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "Search query"},
				"limit": map[string]interface{}{"type": "number", "description": "Results per engine (default 5)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"`
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("search-engines extension starting")
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
		case "search_searxng":
			result = searchSearXNG(ctx, params.Args)
		case "search_querit":
			result = searchQuerit(ctx, params.Args)
		case "search_seltz":
			result = searchSeltz(ctx, params.Args)
		case "search_parallel":
			result = searchParallel(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func searchSearXNG(ctx context.Context, args map[string]interface{}) callToolResult {
	baseURL := os.Getenv("SEARXNG_URL")
	if baseURL == "" {
		return callToolResult{Error: "SEARXNG_URL not set. Set it to your SearXNG instance URL."}
	}
	query, _ := args["query"].(string)
	if query == "" {
		return callToolResult{Error: "query is required"}
	}

	u, err := url.Parse(baseURL + "/search")
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("searxng: invalid base URL: %v", err)}
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	if cats, ok := args["categories"].(string); ok && cats != "" {
		q.Set("categories", cats)
	}
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("searxng: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	json.Unmarshal(body, &data)

	var results []searchResult
	maxResults := getInt(args, "maxResults", 10)
	for i, r := range data.Results {
		if i >= maxResults {
			break
		}
		results = append(results, searchResult{
			Title: r.Title, URL: r.URL, Snippet: truncateStr(r.Content, 300), Source: "searxng",
		})
	}
	return formatResults(results)
}

func searchQuerit(ctx context.Context, args map[string]interface{}) callToolResult {
	apiKey := os.Getenv("QUERIT_API_KEY")
	query, _ := args["query"].(string)
	if query == "" {
		return callToolResult{Error: "query is required"}
	}
	if apiKey == "" {
		// Fallback: inform that API key is needed
		return callToolResult{Success: true, Output: fmt.Sprintf(
			"Querit search configured for: %s\n\nSet QUERIT_API_KEY to enable Querit semantic search.\nQuerit.ai provides AI-powered semantic search at https://querit.ai",
			query,
		)}
	}
	payload := map[string]interface{}{"query": query, "limit": getInt(args, "maxResults", 10)}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.querit.ai/v1/search", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("querit: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(body)}
}

func searchSeltz(ctx context.Context, args map[string]interface{}) callToolResult {
	apiKey := os.Getenv("SELTZ_API_KEY")
	query, _ := args["query"].(string)
	if query == "" {
		return callToolResult{Error: "query is required"}
	}
	if apiKey == "" {
		return callToolResult{Success: true, Output: fmt.Sprintf(
			"Seltz search configured for: %s\n\nSet SELTZ_API_KEY to enable Seltz search.",
			query,
		)}
	}
	u := fmt.Sprintf("https://api.seltz.com/v1/search?q=%s&limit=%d",
		url.QueryEscape(query), getInt(args, "maxResults", 10))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("X-API-Key", apiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("seltz: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return callToolResult{Success: resp.StatusCode < 400, Output: string(body)}
}

func searchParallel(ctx context.Context, args map[string]interface{}) callToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return callToolResult{Error: "query is required"}
	}
	limit := getInt(args, "limit", 5)

	type engineOutput struct {
		Name   string
		Output string
		Error  string
	}

	var wg sync.WaitGroup
	outputs := make(chan engineOutput, 3)

	// SearXNG
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := searchSearXNG(ctx, map[string]interface{}{"query": query, "maxResults": float64(limit)})
		if r.Error != "" {
			outputs <- engineOutput{Name: "searxng", Error: r.Error}
			return
		}
		outputs <- engineOutput{Name: "searxng", Output: r.Output}
	}()

	// DuckDuckGo (HTML)
	wg.Add(1)
	go func() {
		defer wg.Done()
		srs, err := searchDDG(ctx, query, limit)
		if err != nil {
			outputs <- engineOutput{Name: "duckduckgo", Error: err.Error()}
			return
		}
		var out strings.Builder
		for i, sr := range srs {
			out.WriteString(fmt.Sprintf("%d. [%s] %s\n  %s\n  %s\n\n", i+1, sr.Source, sr.Title, sr.URL, sr.Snippet))
		}
		outputs <- engineOutput{Name: "duckduckgo", Output: out.String()}
	}()

	// Querit (if configured)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if os.Getenv("QUERIT_API_KEY") == "" {
			outputs <- engineOutput{Name: "querit", Error: "not configured"}
			return
		}
		r := searchQuerit(ctx, map[string]interface{}{"query": query, "maxResults": float64(limit)})
		if r.Error != "" {
			outputs <- engineOutput{Name: "querit", Error: r.Error}
			return
		}
		outputs <- engineOutput{Name: "querit", Output: r.Output}
	}()

	go func() { wg.Wait(); close(outputs) }()

	var allOutputs []string
	errors := []string{}
	engineCount := 0
	for o := range outputs {
		if o.Error != "" {
			errors = append(errors, fmt.Sprintf("%s: %s", o.Name, o.Error))
			continue
		}
		engineCount++
		allOutputs = append(allOutputs, fmt.Sprintf("--- %s ---\n%s", o.Name, o.Output))
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Parallel search for: %s\n%d engines responded\n\n", query, engineCount))
	if len(errors) > 0 {
		out.WriteString(fmt.Sprintf("Engines unavailable: %s\n\n", strings.Join(errors, ", ")))
	}
	out.WriteString(strings.Join(allOutputs, "\n"))

	return callToolResult{Success: true, Output: out.String()}
}

func searchDDG(ctx context.Context, query string, limit int) ([]searchResult, error) {
	u := fmt.Sprintf("https://lite.duckduckgo.com/lite?q=%s", url.QueryEscape(query))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 500000))
	html := string(body)

	var results []searchResult
	// Simple extraction of result links from DDG lite HTML
	for i := 0; i < limit; i++ {
		linkStart := strings.Index(html, `<a rel="nofollow" href="`)
		if linkStart < 0 {
			break
		}
		html = html[linkStart+26:]
		linkEnd := strings.Index(html, `"`)
		if linkEnd < 0 {
			break
		}
		resultURL := html[:linkEnd]
		html = html[linkEnd:]

		descStart := strings.Index(html, `<td class="result-snippet">`)
		desc := ""
		if descStart >= 0 {
			html = html[descStart+27:]
			descEnd := strings.Index(html, "</td>")
			if descEnd >= 0 {
				desc = stripTags(html[:descEnd])
			}
		}

		results = append(results, searchResult{
			Title: extractDDGTitle(resultURL), URL: resultURL, Snippet: desc, Source: "duckduckgo",
		})
	}
	return results, nil
}

func extractDDGTitle(url string) string {
	if u, err := urlParse(url); err == nil {
		return u.Host + u.Path
	}
	return url
}

func urlParse(s string) (*url.URL, error) { return url.Parse(s) }

func stripTags(s string) string {
	var out strings.Builder
	inTag := false
	for _, c := range s {
		if c == '<' {
			inTag = true
			continue
		}
		if c == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(c)
		}
	}
	return strings.TrimSpace(out.String())
}

func getInt(args map[string]interface{}, key string, fallback int) int {
	v, ok := args[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return fallback
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func formatResults(results []searchResult) callToolResult {
	if len(results) == 0 {
		return callToolResult{Success: true, Output: "No results found."}
	}
	var out strings.Builder
	for i, r := range results {
		out.WriteString(fmt.Sprintf("%d. [%s] %s\n  %s\n  %s\n\n", i+1, r.Source, r.Title, r.URL, r.Snippet))
	}
	out.WriteString(fmt.Sprintf("---\n%d results", len(results)))
	return callToolResult{Success: true, Output: out.String()}
}
