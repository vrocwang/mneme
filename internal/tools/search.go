package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebSearch performs web searches via configurable backends.
type WebSearch struct {
	provider     string // "brave", "tavily", "searxng", or "" for duckduckgo
	apiKey       string
	braveAPIKey  string
	tavilyAPIKey string
	searxngURL   string
	client       *http.Client
}

// NewWebSearch creates a web search tool. Provider is auto-detected from
// whichever API key/URL is set (brave > tavily > searxng > duckduckgo).
func NewWebSearch(braveAPIKey, tavilyAPIKey, searxngURL string) *WebSearch {
	provider := ""
	apiKey := ""
	switch {
	case braveAPIKey != "":
		provider = "brave"
		apiKey = braveAPIKey
	case tavilyAPIKey != "":
		provider = "tavily"
		apiKey = tavilyAPIKey
	case searxngURL != "":
		provider = "searxng"
	}
	return &WebSearch{
		provider:     provider,
		apiKey:       apiKey,
		braveAPIKey:  braveAPIKey,
		tavilyAPIKey: tavilyAPIKey,
		searxngURL:   searxngURL,
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *WebSearch) Schema() Schema {
	return Schema{
		Name:        "web_search",
		Description: "Search the web for information. Returns titles, URLs, and snippets.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
				"count": map[string]interface{}{
					"type":        "integer",
					"description": "Number of results (default 5, max 20)",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *WebSearch) PermissionLevel() PermissionLevel { return PermExecute }
func (t *WebSearch) SideEffects() bool                { return true }

func (t *WebSearch) Execute(ctx context.Context, args map[string]interface{}) Result {
	query, _ := args["query"].(string)
	if query == "" {
		return Result{Error: "query is required"}
	}

	count := 5
	if c, ok := args["count"].(float64); ok && c > 0 {
		count = int(c)
		if count > 20 {
			count = 20
		}
	}

	switch t.provider {
	case "brave":
		return t.searchBrave(ctx, query, count)
	case "tavily":
		return t.searchTavily(ctx, query, count)
	case "searxng":
		return t.searchSearxNG(ctx, query, count)
	default:
		return t.searchDuckDuckGo(ctx, query, count)
	}
}

func (t *WebSearch) searchBrave(ctx context.Context, query string, count int) Result {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.search.brave.com/res/v1/web/search", nil)
	if err != nil {
		return Result{Error: fmt.Sprintf("brave request: %v", err)}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", t.apiKey)
	req.URL.RawQuery = fmt.Sprintf("q=%s&count=%d", url.QueryEscape(query), count)

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("brave search: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return Result{Error: fmt.Sprintf("brave search: status %d", resp.StatusCode)}
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{Error: fmt.Sprintf("parse results: %v", err)}
	}

	var sr []searchResult
	for _, r := range result.Web.Results {
		sr = append(sr, searchResult{Title: r.Title, URL: r.URL, Description: r.Description})
	}
	return formatSearchResults(query, sr)
}

func (t *WebSearch) searchTavily(ctx context.Context, query string, count int) Result {
	body := map[string]interface{}{
		"api_key":        t.apiKey,
		"query":          query,
		"search_depth":   "basic",
		"max_results":    count,
		"include_answer": true,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Result{Error: fmt.Sprintf("marshal search request: %v", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", strings.NewReader(string(jsonBody)))
	if err != nil {
		return Result{Error: fmt.Sprintf("tavily request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("tavily search: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return Result{Error: fmt.Sprintf("tavily search: status %d: %s", resp.StatusCode, string(body))}
	}

	var result struct {
		Answer  string `json:"answer"`
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{Error: fmt.Sprintf("parse results: %v", err)}
	}

	var b strings.Builder
	if result.Answer != "" {
		b.WriteString("Answer: " + result.Answer + "\n\n")
	}
	for i, r := range result.Results {
		b.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Content))
	}
	return Result{Success: true, Output: b.String()}
}

func (t *WebSearch) searchSearxNG(ctx context.Context, query string, count int) Result {
	req, err := http.NewRequestWithContext(ctx, "GET", t.searxngURL+"/search", nil)
	if err != nil {
		return Result{Error: fmt.Sprintf("searxng request: %v", err)}
	}
	req.Header.Set("Accept", "application/json")
	req.URL.RawQuery = fmt.Sprintf("format=json&q=%s", url.QueryEscape(query))

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("searxng search: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return Result{Error: fmt.Sprintf("searxng search: status %d", resp.StatusCode)}
	}

	var result struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{Error: fmt.Sprintf("parse results: %v", err)}
	}

	var sr []searchResult
	for i, r := range result.Results {
		if i >= count {
			break
		}
		sr = append(sr, searchResult{Title: r.Title, URL: r.URL, Description: r.Content})
	}
	return formatSearchResults(query, sr)
}

func (t *WebSearch) searchDuckDuckGo(ctx context.Context, query string, count int) Result {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://html.duckduckgo.com/html/", nil)
	if err != nil {
		return Result{Error: fmt.Sprintf("duckduckgo request: %v", err)}
	}
	req.URL.RawQuery = fmt.Sprintf("q=%s", url.QueryEscape(query))
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Mneme/1.0)")

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("duckduckgo search: %v", err)}
	}
	defer resp.Body.Close()

	html, err := io.ReadAll(io.LimitReader(resp.Body, 500000))
	if err != nil {
		return Result{Error: fmt.Sprintf("duckduckgo read: %v", err)}
	}

	// Simple HTML extraction for DuckDuckGo results
	results := extractDuckDuckGoResults(string(html), count)
	if len(results) == 0 {
		// Distinguish between a truly empty result set and a parsing failure
		// by checking whether the HTML contains result markers at all.
		if strings.Contains(string(html), "result__") {
			return Result{Success: true, Output: fmt.Sprintf("No results found for: %s", query)}
		}
		return Result{Error: "failed to parse search results — DuckDuckGo may have changed its HTML structure"}
	}
	return formatSearchResults(query, results)
}

type searchResult struct {
	Title       string
	URL         string
	Description string
}

func extractDuckDuckGoResults(html string, max int) []searchResult {
	var results []searchResult
	for i := 0; i < max; i++ {
		// Find the title/link anchor first — in DuckDuckGo's HTML,
		// result__a appears BEFORE result__snippet within each result block.
		anchorStart := strings.Index(html, `class="result__a"`)
		if anchorStart < 0 {
			break
		}

		// Extract URL from href attribute
		linkURL := ""
		hrefStart := strings.Index(html[anchorStart:], `href="`)
		if hrefStart >= 0 {
			hrefStart += anchorStart + 6
			hrefEnd := strings.Index(html[hrefStart:], `"`)
			if hrefEnd >= 0 {
				linkURL = strings.TrimSpace(html[hrefStart : hrefStart+hrefEnd])
			}
		}

		// Extract title text from inside the anchor tag
		title := ""
		tagEnd := strings.Index(html[anchorStart:], ">")
		if tagEnd >= 0 {
			titleTextStart := anchorStart + tagEnd + 1
			titleEnd := strings.Index(html[titleTextStart:], "<")
			if titleEnd >= 0 {
				title = strings.TrimSpace(html[titleTextStart : titleTextStart+titleEnd])
			}
		}

		// Find the snippet that follows this anchor within the same result block
		snippetStart := strings.Index(html[anchorStart:], `class="result__snippet"`)
		desc := ""
		if snippetStart >= 0 {
			snippetStart += anchorStart
			descTagEnd := strings.Index(html[snippetStart:], ">")
			if descTagEnd >= 0 {
				descTextStart := snippetStart + descTagEnd + 1
				descEnd := strings.Index(html[descTextStart:], "<")
				if descEnd >= 0 {
					desc = strings.TrimSpace(html[descTextStart : descTextStart+descEnd])
				}
				// Advance past this snippet so the next iteration sees the next result
				html = html[descTextStart+descEnd:]
			} else {
				html = html[snippetStart+len(`class="result__snippet"`):]
			}
		} else {
			// No snippet found for this anchor; skip past it so we can continue
			html = html[anchorStart+len(`class="result__a"`):]
		}

		if desc != "" || title != "" {
			results = append(results, searchResult{
				Title:       title,
				URL:         linkURL,
				Description: desc,
			})
		}
	}
	return results
}

func formatSearchResults(query string, results []searchResult) Result {
	if len(results) == 0 {
		return Result{Success: true, Output: fmt.Sprintf("No results found for: %s", query)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Search results for: %s\n\n", query))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description))
	}
	return Result{Success: true, Output: b.String()}
}
