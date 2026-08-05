package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// webFetchTool fetches and cleans content from a web page.
// Falls back to http.Get if Chrome is not available, but prefers CDP for JS-rendered content.
func webFetchTool(ctx context.Context, args map[string]interface{}) callToolResult {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return callToolResult{Error: "url is required"}
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	if err := validateSafeURL(rawURL); err != nil {
		return callToolResult{Error: fmt.Sprintf("URL rejected: %v", err)}
	}

	maxChars := 10000
	if mc, ok := intFromArgs(args, "maxChars"); ok && mc > 0 {
		maxChars = mc
	}

	includeHTML := false
	if ih, ok := args["includeHTML"].(bool); ok {
		includeHTML = ih
	}

	// Try CDP first; fall back to plain HTTP if Chrome is unavailable
	if hasChrome() {
		return webFetchCDP(ctx, rawURL, maxChars, includeHTML)
	}
	return webFetchHTTP(ctx, rawURL, maxChars, includeHTML)
}

// webFetchCDP uses Chrome to fetch and render the page, then extracts clean content.
func webFetchCDP(ctx context.Context, rawURL string, maxChars int, includeHTML bool) callToolResult {
	// Simplified: use the browser tool and reformat output.
	result := browserTool(ctx, map[string]interface{}{"url": rawURL})
	if !result.Success {
		// Fall back to HTTP on CDP failure
		return webFetchHTTP(ctx, rawURL, maxChars, includeHTML)
	}

	out := result.Output
	if len(out) > maxChars {
		out = out[:maxChars] + fmt.Sprintf("\n\n[Truncated at %d chars]", maxChars)
	}

	return callToolResult{Success: true, Output: out}
}

// webFetchHTTP fetches a URL via plain HTTP with readability extraction.
func webFetchHTTP(ctx context.Context, rawURL string, maxChars int, includeHTML bool) callToolResult {
	if err := validateSafeURL(rawURL); err != nil {
		return callToolResult{Error: fmt.Sprintf("URL rejected: %v", err)}
	}

	client := newSafeHTTPClient(30 * time.Second)

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Mneme/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("fetch: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5MB limit
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("read body: %v", err)}
	}

	text := extractReadableText(string(body))
	title := extractTitle(string(body))

	var out strings.Builder
	out.WriteString(fmt.Sprintf("URL: %s\n", rawURL))
	out.WriteString(fmt.Sprintf("Status: %d\n", resp.StatusCode))
	if title != "" {
		out.WriteString(fmt.Sprintf("Title: %s\n", title))
	}
	out.WriteString(fmt.Sprintf("Content-Type: %s\n\n", resp.Header.Get("Content-Type")))

	content := text
	if len(content) > maxChars {
		content = content[:maxChars] + fmt.Sprintf("\n\n[Truncated at %d chars; total: %d]", maxChars, len(text))
	}
	out.WriteString(content)

	if includeHTML {
		out.WriteString("\n\n--- Raw HTML (first 2000 chars) ---\n")
		htmlStr := string(body)
		out.WriteString(truncateStr(htmlStr, 2000))
	}

	return callToolResult{Success: true, Output: out.String()}
}

// ── SSRF-safe HTTP client ───────────────────────────────────────────────

func newSafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

// validateSafeURL checks a URL for basic safety (scheme and DNS resolution).
func validateSafeURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("no host in URL")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("URL resolves to private/internal address %s", ip)
		}
	}
	return nil
}
