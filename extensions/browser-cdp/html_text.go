package main

import "strings"

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
