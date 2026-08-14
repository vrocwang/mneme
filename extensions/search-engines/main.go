// Search Engines extension for Mneme.
//
// Provides additional search backends beyond the core web_search:
//   - search_searxng: search via a self-hosted SearXNG instance
//   - search_querit: search via Querit.ai semantic API
//   - search_seltz: search via Seltz engine
//   - search_parallel: search across multiple engines concurrently
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
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "search-engines",
		Version:     "0.1.0",
		Description: "Additional search backends: SearXNG, Querit, Seltz, Parallel",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, searchSearXNG)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, searchQuerit)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, searchSeltz)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, searchParallel)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "search-engines: %v\n", err)
		os.Exit(1)
	}
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"`
}

func searchSearXNG(ctx context.Context, args map[string]interface{}) extsdk.Result {
	baseURL := os.Getenv("SEARXNG_URL")
	if baseURL == "" {
		return extsdk.Result{Error: "SEARXNG_URL not set. Set it to your SearXNG instance URL."}
	}
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
	}

	u, err := url.Parse(baseURL + "/search")
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("searxng: invalid base URL: %v", err)}
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
		return extsdk.Result{Error: fmt.Sprintf("searxng: %v", err)}
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

func searchQuerit(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiKey := os.Getenv("QUERIT_API_KEY")
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
	}
	if apiKey == "" {
		// Fallback: inform that API key is needed
		return extsdk.Result{Success: true, Output: fmt.Sprintf(
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
		return extsdk.Result{Error: fmt.Sprintf("querit: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(body)}
}

func searchSeltz(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiKey := os.Getenv("SELTZ_API_KEY")
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
	}
	if apiKey == "" {
		return extsdk.Result{Success: true, Output: fmt.Sprintf(
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
		return extsdk.Result{Error: fmt.Sprintf("seltz: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: string(body)}
}

func searchParallel(ctx context.Context, args map[string]interface{}) extsdk.Result {
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
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

	return extsdk.Result{Success: true, Output: out.String()}
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

func formatResults(results []searchResult) extsdk.Result {
	if len(results) == 0 {
		return extsdk.Result{Success: true, Output: "No results found."}
	}
	var out strings.Builder
	for i, r := range results {
		out.WriteString(fmt.Sprintf("%d. [%s] %s\n  %s\n  %s\n\n", i+1, r.Source, r.Title, r.URL, r.Snippet))
	}
	out.WriteString(fmt.Sprintf("---\n%d results", len(results)))
	return extsdk.Result{Success: true, Output: out.String()}
}
