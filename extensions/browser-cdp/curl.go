package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// curlTool makes a raw HTTP request with full control over method, headers, and body.
// It includes SSRF protection to prevent requests to internal/private addresses.
func curlTool(ctx context.Context, args map[string]interface{}) extsdk.Result {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return extsdk.Result{Error: "url is required"}
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	// SSRF protection
	if err := validateSafeURL(rawURL); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("URL rejected: %v", err)}
	}

	method := "GET"
	if m, ok := args["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}

	var bodyReader io.Reader
	if bodyStr, ok := args["body"].(string); ok && bodyStr != "" {
		bodyReader = strings.NewReader(bodyStr)
	}

	timeout := 30.0
	if t, ok := floatFromArgs(args, "timeout"); ok && t > 0 {
		timeout = t
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// Re-validate redirect targets against SSRF
			if err := validateSafeURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("create request: %v", err)}
	}

	req.Header.Set("User-Agent", "Mneme-Curl/1.0")

	// Parse custom headers from args
	if headers, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				req.Header.Set(k, vs)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB limit
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("read body: %v", err)}
	}

	// Build curl-style output
	var out strings.Builder
	out.WriteString(fmt.Sprintf("%s %s\n", method, rawURL))
	out.WriteString(fmt.Sprintf("Status: %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode)))

	// Response headers
	for k, vs := range resp.Header {
		for _, v := range vs {
			out.WriteString(fmt.Sprintf("%s: %s\n", k, v))
		}
	}

	out.WriteString(fmt.Sprintf("\n%s", string(body)))

	result := out.String()
	if len(result) > 50000 {
		result = result[:50000] + fmt.Sprintf("\n\n[Response truncated at 50000 chars. Total: %d chars]", len(out.String()))
	}

	return extsdk.Result{Success: true, Output: result}
}

// ── Shared HTML utilities ────────────────────────────────────────────────

// extractReadableText converts HTML to readable plain text using a single-pass state machine.
func extractReadableText(html string) string {
	var result strings.Builder
	result.Grow(len(html) / 4)

	inTag := false
	inScript := false
	inStyle := false
	tagName := ""
	collectingTag := false

	for i := 0; i < len(html); i++ {
		c := html[i]
		switch {
		case c == '<':
			inTag = true
			tagName = ""
			collectingTag = true
		case c == '>' && inTag:
			inTag = false
			collectingTag = false
			lower := strings.ToLower(tagName)
			if strings.HasPrefix(lower, "script") {
				inScript = true
			} else if strings.HasPrefix(lower, "/script") {
				inScript = false
			} else if strings.HasPrefix(lower, "style") {
				inStyle = true
			} else if strings.HasPrefix(lower, "/style") {
				inStyle = false
			}
			if !inScript && !inStyle {
				result.WriteByte(' ')
			}
			tagName = ""
		case inTag:
			if collectingTag && ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '/' || c == '!') {
				tagName += string(c)
			} else if c == ' ' {
				collectingTag = false
			}
		default:
			if !inTag && !inScript && !inStyle {
				result.WriteByte(c)
			}
		}
	}

	cleaned := collapseWhitespace(result.String())
	return cleaned
}

func extractTitle(html string) string {
	lower := strings.ToLower(html)
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	contentStart := strings.Index(html[start:], ">") + start + 1
	contentEnd := strings.Index(strings.ToLower(html[contentStart:]), "</title>")
	if contentEnd < 0 {
		return ""
	}
	return strings.TrimSpace(html[contentStart : contentStart+contentEnd])
}

func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n")
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// isPrivateIP checks if an IP is private, loopback, or otherwise internal.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast()
}
