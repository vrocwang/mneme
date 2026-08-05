// Package approval provides argument redaction for approval prompts.
// Anything written to pending_approvals or broadcast on the event bus
// must be scrubbed first — no home-directory paths, no user_ids,
// no raw message bodies, contact names, subjects, or addresses.
package approval

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// sensitiveKeys lists field names whose values are assumed to contain raw
// user content or PII and MUST be redacted. Matching is case-insensitive.
var sensitiveKeys = []string{
	"body", "content", "text", "message", "messages",
	"html", "html_body", "snippet", "subject", "title",
	"recipient", "recipients", "to", "cc", "bcc", "from", "sender",
	"address", "email", "phone", "contact", "contacts",
	"name", "first_name", "last_name", "full_name", "channel_name",
	"user", "user_id", "userid", "username",
	"thread_id", "thread_ts", "conversation_id",
	"token", "api_key", "secret", "password", "authorization", "auth",
}

// safeSummaryKeys lists field names safe to include in action summaries.
var safeSummaryKeys = []string{
	"action", "tool_slug", "action_name",
	"integration", "app", "provider",
	"channel", "method", "endpoint",
}

// RedactArgs returns a deep-cloned copy of args with all sensitive fields
// replaced by redaction markers. Unknown fields pass through unchanged so
// the UI can still show useful context (tool name, integration id, etc.).
func RedactArgs(args map[string]interface{}) map[string]interface{} {
	return walkMap(args).(map[string]interface{})
}

// RedactArgsJSON parses a JSON string, redacts it, and returns the
// redacted JSON string. If parsing fails, returns a safe fallback.
func RedactArgsJSON(argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return `{"<redacted>":"unparseable args"}`
	}
	redacted := RedactArgs(args)
	out, err := json.Marshal(redacted)
	if err != nil {
		return `{"<redacted>":"marshal error"}`
	}
	return string(out)
}

// SummarizeAction builds a short human-readable summary of a tool call.
// Pulls a handful of safe fields and tacks on a byte-count hint so the
// user knows *what* the agent wants to do without exposing content.
func SummarizeAction(toolName string, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("%s (unparseable args)", toolName)
	}

	var parts []string
	for _, key := range safeSummaryKeys {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				parts = append(parts, fmt.Sprintf("%s=%s", key, s))
			}
		}
	}

	byteCount := len(argsJSON)
	if len(parts) == 0 {
		return fmt.Sprintf("%s (%d bytes of arguments)", toolName, byteCount)
	}
	return fmt.Sprintf("%s(%s, %d bytes)", toolName, strings.Join(parts, ", "), byteCount)
}

// ── walk helpers ─────────────────────────────────────────────────────

func walkMap(m map[string]interface{}) interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if isSensitiveKey(k) {
			out[k] = redactValue(v)
		} else {
			out[k] = walkValue(v)
		}
	}
	return out
}

func walkSlice(items []interface{}) interface{} {
	out := make([]interface{}, len(items))
	for i, v := range items {
		out[i] = walkValue(v)
	}
	return out
}

func walkValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return walkMap(val)
	case []interface{}:
		return walkSlice(val)
	case string:
		return scrubPaths(val)
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if lower == s {
			return true
		}
	}
	return false
}

func redactValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("<redacted: string (%d chars)>", utf8.RuneCountInString(val))
	case []interface{}:
		return fmt.Sprintf("<redacted: array (%d items)>", len(val))
	case map[string]interface{}:
		return fmt.Sprintf("<redacted: object (%d keys)>", len(val))
	case float64, json.Number:
		return "<redacted: number>"
	case bool:
		return "<redacted: bool>"
	case nil:
		return nil
	default:
		return "<redacted: unknown>"
	}
}

// ── path scrubbing ───────────────────────────────────────────────────

// scrubPaths replaces absolute home-directory paths in a string with
// <HOME> markers so the user's username is not leaked on multi-tenant
// log shipping. Handles /Users/<name>/..., /home/<name>/..., and
// C:\Users\<name>\... patterns.
func scrubPaths(input string) string {
	if !strings.Contains(input, "Users") && !strings.Contains(input, "home") {
		return input
	}

	var out strings.Builder
	out.Grow(len(input))
	i := 0
	runes := []rune(input)

	for i < len(runes) {
		if prefixLen := matchHomePrefixRunes(runes[i:]); prefixLen > 0 {
			out.WriteString("<HOME>")
			i += prefixLen
			// Skip past the username segment up to the next path separator
			// (or end of input).
			rest := runes[i:]
			slashIdx := -1
			for j, r := range rest {
				if r == '/' || r == '\\' {
					slashIdx = j
					break
				}
			}
			if slashIdx >= 0 {
				i += slashIdx
			} else {
				i = len(runes)
			}
		} else {
			out.WriteRune(runes[i])
			i++
		}
	}
	return out.String()
}

func matchHomePrefixRunes(rest []rune) int {
	startsWith := func(needle string) bool {
		n := []rune(needle)
		if len(rest) < len(n) {
			return false
		}
		for i, r := range n {
			if rest[i] != r && rest[i] != r+32 && rest[i] != r-32 {
				// Simple case-insensitive ASCII check; sufficient for path prefixes.
				if !eqFoldASCII(rest[i], r) {
					return false
				}
			}
		}
		return true
	}

	if startsWith("/Users/") {
		return len([]rune("/Users/"))
	}
	if startsWith("/home/") {
		return len([]rune("/home/"))
	}
	// Windows: <drive>:\Users\
	if len(rest) >= 9 &&
		isASCIIAlpha(rest[0]) &&
		rest[1] == ':' &&
		rest[2] == '\\' &&
		eqFoldASCIISlice(rest[3:9], []rune("Users\\")) {
		return 9
	}
	return 0
}

func eqFoldASCII(a, b rune) bool {
	if a >= 'A' && a <= 'Z' {
		a += 32
	}
	if b >= 'A' && b <= 'Z' {
		b += 32
	}
	return a == b
}

func eqFoldASCIISlice(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !eqFoldASCII(a[i], b[i]) {
			return false
		}
	}
	return true
}

func isASCIIAlpha(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}
