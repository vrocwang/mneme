package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Browser provides basic web browsing capabilities.
type Browser struct {
	client      *http.Client
	timeoutSecs int
}

// NewBrowser creates a browser tool with SSRF-safe redirect handling.
// timeoutSecs is the HTTP client timeout in seconds. If 0, defaults to 30.
func NewBrowser(timeoutSecs int) *Browser {
	if timeoutSecs <= 0 {
		timeoutSecs = 30
	}
	return &Browser{
		timeoutSecs: timeoutSecs,
		client: &http.Client{
			Timeout: time.Duration(timeoutSecs) * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				if err := validateURLFn(req.URL.String()); err != nil {
					return fmt.Errorf("redirect blocked: %w", err)
				}
				return nil
			},
		}}
}

func (t *Browser) Schema() Schema {
	return Schema{
		Name:        "browser",
		Description: "Open a URL and read its content as readable text",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to open",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *Browser) PermissionLevel() PermissionLevel { return PermExecute }
func (t *Browser) SideEffects() bool                { return true }

func (t *Browser) Execute(ctx context.Context, args map[string]interface{}) Result {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return Result{Error: "url is required"}
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	// SSRF protection — reuse the same URL validator as network tools.
	if err := validateURLFn(rawURL); err != nil {
		return Result{Error: fmt.Sprintf("url rejected: %v", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return Result{Error: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Mneme/1.0)")

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("fetch: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB limit
	if err != nil {
		return Result{Error: fmt.Sprintf("read: %v", err)}
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
	out.WriteString(truncateString(text, 5000))
	if len(text) > 5000 {
		out.WriteString(fmt.Sprintf("\n\n[Content truncated: %d total characters]", len(text)))
	}

	return Result{Success: true, Output: out.String()}
}

// extractReadableText removes HTML tags and scripts, returning plain text.
// Uses a single-pass state machine (O(n)) instead of repeated substring removal (O(n²)).
func extractReadableText(html string) string {
	var result strings.Builder
	result.Grow(len(html) / 4) // pre-allocate ~25% of input size

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
			// Check if entering/exiting script or style block
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
			// Character content — skip if inside script/style
			if !inTag && !inScript && !inStyle {
				result.WriteByte(c)
			}
		}
	}

	// Clean up whitespace and entities
	cleaned := result.String()
	cleaned = decodeHTMLEntities(cleaned)

	// Collapse whitespace
	lines := strings.Split(cleaned, "\n")
	var outLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			outLines = append(outLines, trimmed)
		}
	}

	return strings.Join(outLines, "\n")
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

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// decodeHTMLEntities decodes common HTML entities including numeric character
// references (decimal &#123; and hex &#x27;) and named entities.
func decodeHTMLEntities(s string) string {
	named := map[string]string{
		"&amp;": "&", "&lt;": "<", "&gt;": ">",
		"&quot;": "\"", "&#39;": "'", "&apos;": "'",
		"&nbsp;": " ", "&copy;": "©", "&reg;": "®",
		"&mdash;": "—", "&ndash;": "–",
		"&lsquo;": "‘", "&rsquo;": "’",
		"&ldquo;": "“", "&rdquo;": "”",
	}
	for entity, replacement := range named {
		s = strings.ReplaceAll(s, entity, replacement)
	}
	// Decode numeric character references: &#NNN; and &#xHHH;
	s = decodeNumericEntities(s)
	return s
}

func decodeNumericEntities(s string) string {
	var result strings.Builder
	result.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '&' && i+2 < len(s) && s[i+1] == '#' {
			end := strings.IndexByte(s[i:], ';')
			if end < 0 {
				result.WriteByte(s[i])
				i++
				continue
			}
			entity := s[i : i+end+1]
			var decoded string
			if len(entity) > 3 && entity[2] == 'x' {
				// Hex: &#xHH;
				if n, ok := parseHexEntity(entity[3 : len(entity)-1]); ok {
					decoded = string(rune(n))
				}
			} else {
				// Decimal: &#NNN;
				if n, ok := parseDecEntity(entity[2 : len(entity)-1]); ok {
					decoded = string(rune(n))
				}
			}
			if decoded != "" {
				result.WriteString(decoded)
			} else {
				result.WriteString(entity)
			}
			i += end + 1
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

func parseDecEntity(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func parseHexEntity(s string) (int, bool) {
	n := 0
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			n = n*16 + int(c-'0')
		case c >= 'a' && c <= 'f':
			n = n*16 + int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			n = n*16 + int(c-'A') + 10
		default:
			return 0, false
		}
	}
	return n, true
}
