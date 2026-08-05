package sync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// WebPageConnector syncs a single web page into the memory pipeline.
// It fetches the URL, strips HTML tags/scripts/styles, and extracts
// readable text content. An optional CSS-style selector can narrow
// the extracted content to a specific section of the page.
type WebPageConnector struct {
	pageURL    string
	selector   string // optional: approximate CSS selector for content extraction
	lastETag   string
	lastMod    string
	httpClient *http.Client
}

// NewWebPageConnector creates a connector for a web page URL.
// selector is optional — when set, only content within matching
// HTML elements is extracted (basic string matching, not a full CSS parser).
func NewWebPageConnector(pageURL string, selector string) *WebPageConnector {
	return &WebPageConnector{
		pageURL:    pageURL,
		selector:   selector,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *WebPageConnector) Name() string {
	u, err := url.Parse(c.pageURL)
	if err != nil {
		return "web:" + c.pageURL
	}
	return "web:" + u.Hostname()
}

func (c *WebPageConnector) Sync(ctx context.Context) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("webpage request: %w", err)
	}
	req.Header.Set("User-Agent", "Mneme/1.0")

	// Conditional GET headers.
	if c.lastETag != "" {
		req.Header.Set("If-None-Match", c.lastETag)
	}
	if c.lastMod != "" {
		req.Header.Set("If-Modified-Since", c.lastMod)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webpage fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webpage fetch HTTP %d", resp.StatusCode)
	}

	// Store conditional headers for next request.
	c.lastETag = resp.Header.Get("ETag")
	c.lastMod = resp.Header.Get("Last-Modified")

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("webpage read body: %w", err)
	}

	text := extractText(string(data), c.selector)
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	return []Item{{
		Source:   c.Name(),
		Path:     c.pageURL,
		Content:  text,
		Modified: time.Now(),
	}}, nil
}

// ── HTML text extraction ─────────────────────────────────────────────

// extractText strips HTML tags, scripts, and styles from raw HTML and
// returns a cleaned plain-text representation. If a CSS selector-like
// string is provided (e.g. "div.content", "#main", ".article-body"),
// only content within matching elements is extracted.
func extractText(html string, selector string) string {
	html = strings.ReplaceAll(html, "\r\n", "\n")

	// Remove script and style blocks with their content.
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = scriptRe.ReplaceAllString(html, "")
	html = styleRe.ReplaceAllString(html, "")

	// Remove HTML comments.
	commentRe := regexp.MustCompile(`<!--.*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	// If a selector is provided, try to isolate content within matching elements.
	if selector != "" {
		html = extractBySelector(html, selector)
	}

	// Remove remaining HTML tags.
	tagRe := regexp.MustCompile(`<[^>]*>`)
	text := tagRe.ReplaceAllString(html, " ")

	// Decode common HTML entities.
	text = htmlEntities(text)

	// Collapse whitespace.
	wsRe := regexp.MustCompile(`[ \t]+`)
	text = wsRe.ReplaceAllString(text, " ")

	nlRe := regexp.MustCompile(`\n{3,}`)
	text = nlRe.ReplaceAllString(text, "\n\n")

	text = strings.TrimSpace(text)
	return text
}

// extractBySelector performs basic CSS selector matching to isolate content.
// It supports: tag names ("div"), class selectors (".class"), ID selectors ("#id"),
// and tag.class combinations ("div.class"). This is a simple string-based
// approximation — it does not handle full CSS selector syntax.
func extractBySelector(html string, selector string) string {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return html
	}

	var tagName, className, idName string

	// Parse selector: id (#foo), class (.bar), or tag (div) / tag.class (div.bar)
	if strings.HasPrefix(selector, "#") {
		idName = selector[1:]
	} else if strings.HasPrefix(selector, ".") {
		className = selector[1:]
	} else if dotIdx := strings.IndexByte(selector, '.'); dotIdx >= 0 {
		tagName = selector[:dotIdx]
		className = selector[dotIdx+1:]
	} else {
		tagName = selector
	}

	// Build a regex to find opening tags matching the selector.
	var openPattern string
	if idName != "" {
		openPattern = fmt.Sprintf(`(?i)<[^>]*\bid\s*=\s*["']?%s[^>]*>`, regexp.QuoteMeta(idName))
	} else if className != "" && tagName != "" {
		openPattern = fmt.Sprintf(`(?i)<%s[^>]*\bclass\s*=\s*["'][^"']*\b%s\b[^"']*["'][^>]*>`, regexp.QuoteMeta(tagName), regexp.QuoteMeta(className))
	} else if className != "" {
		openPattern = fmt.Sprintf(`(?i)<[^>]*\bclass\s*=\s*["'][^"']*\b%s\b[^"']*["'][^>]*>`, regexp.QuoteMeta(className))
	} else {
		openPattern = fmt.Sprintf(`(?i)<%s[>\s]`, regexp.QuoteMeta(tagName))
	}

	openRe := regexp.MustCompile(openPattern)
	loc := openRe.FindStringIndex(html)
	if loc == nil {
		return html // no match, return full HTML
	}

	startIdx := loc[0]

	// Find the matching close tag by counting nesting depth.
	// We need the bare tag name (no attributes) for the close tag.
	bareTag := tagName
	if bareTag == "" {
		// Try to extract tag name from the matched opening tag.
		tagMatch := regexp.MustCompile(`(?i)<(\w+)`).FindStringSubmatch(html[loc[0]:loc[1]])
		if len(tagMatch) >= 2 {
			bareTag = tagMatch[1]
		} else {
			return html[startIdx:]
		}
	}

	closePattern := fmt.Sprintf(`(?i)</%s\s*>`, regexp.QuoteMeta(bareTag))
	closeRe := regexp.MustCompile(closePattern)

	// Count nesting depth from the opening tag onward.
	depth := 0
	searchFrom := startIdx + 1
	for {
		openIdx := openRe.FindStringIndex(html[searchFrom:])
		nextOpen := -1
		if openIdx != nil {
			nextOpen = searchFrom + openIdx[0]
		}

		closeIdx := closeRe.FindStringIndex(html[searchFrom:])
		nextClose := -1
		if closeIdx != nil {
			nextClose = searchFrom + closeIdx[0]
		}

		if nextClose == -1 {
			// No more close tags; return from start to end.
			return html[startIdx:]
		}

		if nextOpen != -1 && nextOpen < nextClose {
			depth++
			searchFrom = nextOpen + 1
		} else {
			if depth == 0 {
				endIdx := nextClose + len(closeRe.FindString(html[nextClose:]))
				return html[startIdx:endIdx]
			}
			depth--
			searchFrom = nextClose + 1
		}
	}
}

// htmlEntities decodes the most common HTML entities to plain text.
func htmlEntities(s string) string {
	repl := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
		"&mdash;", "--",
		"&ndash;", "-",
		"&lsquo;", "'",
		"&rsquo;", "'",
		"&ldquo;", "\"",
		"&rdquo;", "\"",
		"&hellip;", "...",
		"&copy;", "(c)",
		"&reg;", "(R)",
		"&trade;", "(TM)",
	)
	return repl.Replace(s)
}

// Ensure interface compliance.
var _ Connector = (*WebPageConnector)(nil)
