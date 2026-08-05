package client

import (
	"regexp"
	"strings"
	"unicode"
)

// maxToolDescriptionLen is the maximum length for a sanitized MCP tool
// description. Longer descriptions are truncated with an ellipsis marker.
const maxToolDescriptionLen = 1024

var (
	// controlChars matches ASCII control characters (0x00-0x1F, 0x7F)
	// except for \n, \r, \t which are rendered as spaces.
	controlChars = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)

	// instructionFence matches common prompt-injection attack patterns that
	// MCP server descriptions could contain.
	instructionFence = regexp.MustCompile(
		`(?i)(ignore\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|directives?|commands?|prompts?)|` +
			`you\s+are\s+(now|instead|actually)\s|` +
			`forget\s+(everything|all)\s+(you\s+know|before)|` +
			`system\s+prompt\s+(override|injection)|` +
			`\[system\].*\[/system\]|` +
			`<\|im_start\|>|<\|im_end\|>)`,
	)

	// whitespaceCollapse matches runs of 3+ whitespace chars.
	whitespaceCollapse = regexp.MustCompile(`\s{3,}`)
)

// SanitizeForLLM cleans an MCP server tool description so it is safe to
// pass to an LLM as part of a function-calling schema. It removes control
// characters, strips instruction-fence patterns, collapses excessive
// whitespace, and truncates at maxToolDescriptionLen.
func SanitizeForLLM(desc string) string {
	if desc == "" {
		return desc
	}

	// Strip ASCII control chars (except \n, \r, \t which become spaces).
	cleaned := controlChars.ReplaceAllString(desc, "")
	cleaned = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, cleaned)

	// Remove instruction-fence patterns.
	cleaned = instructionFence.ReplaceAllString(cleaned, "")

	// Collapse runs of whitespace.
	cleaned = whitespaceCollapse.ReplaceAllString(cleaned, "  ")
	cleaned = strings.TrimSpace(cleaned)

	// Truncate at length limit.
	if len(cleaned) > maxToolDescriptionLen {
		cleaned = cleaned[:maxToolDescriptionLen-3] + "..."
	}

	// Ensure we don't return empty after sanitization.
	if cleaned == "" {
		return "(no description)"
	}

	return cleaned
}

// SanitizeTools sanitizes all tool descriptions in a slice and returns a
// new slice. Tools whose names contain control characters or instruction
// fences are dropped entirely (their description is replaced with a
// warning so the LLM knows to avoid them).
func SanitizeTools(tools []Tool) []Tool {
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		st := t
		st.Description = SanitizeForLLM(st.Description)

		// Drop tools whose name itself looks like an injection attempt.
		if instructionFence.MatchString(st.Name) || !isSafeName(st.Name) {
			continue
		}
		out = append(out, st)
	}
	return out
}

// isSafeName returns false if the name contains non-printable characters
// or suspiciously long segments that could hide injection payloads.
func isSafeName(name string) bool {
	if len(name) > 256 {
		return false
	}
	for _, r := range name {
		if !unicode.IsPrint(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
