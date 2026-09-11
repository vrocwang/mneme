package agent

import (
	"strings"
	"time"
)

// ── Shared types used by ChatService, Runner, Callbacks, and post-turn hooks ──

// ToolCallResult captures the outcome of a single tool execution.
type ToolCallResult struct {
	Name     string
	Args     map[string]interface{}
	Output   string
	Error    string
	Duration time.Duration
}

// TurnResult is the complete outcome of a single agent turn.
type TurnResult struct {
	ThreadID      string
	Model         string
	Response      string
	ToolCalls     []ToolCallResult
	Error         error
	Rounds        int
	TotalDuration time.Duration
	InputTokens   int
	OutputTokens  int
	CostCents     int
}

// StreamEvent is emitted to the Wails frontend during streaming.
type StreamEvent struct {
	ThreadID string `json:"threadId"`
	Type     string `json:"type"` // "token", "thinking", "tool_call", "tool_result", "done", "error"
	Content  string `json:"content"`
	Args     string `json:"args,omitempty"`
	Done     bool   `json:"done"`
}

// GeneralAgentID is the ID of the top-level orchestrator agent. It is the
// roster entry the general agent is built from and the identity recorded on
// session and ledger rows.
const GeneralAgentID = "general"

// DefaultCredentialScrubber masks known credential patterns from output.
func DefaultCredentialScrubber(output string) string {
	for _, prefix := range credentialPatterns {
		output = scrubPrefix(output, prefix)
	}
	return output
}

var credentialPatterns = []string{
	"sk-", "ghp_", "xoxb-", "xoxp-", "xapp-",
	"Bearer ", "Authorization:", "-----BEGIN",
}

func scrubPrefix(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		idx := strings.Index(line, prefix)
		if idx >= 0 {
			// At line start for auth prefixes; anywhere for key-like prefixes.
			if prefix == "Authorization:" || prefix == "Bearer " {
				if idx == 0 || (idx > 0 && line[idx-1] == ' ') {
					end := min(len(line), idx+40)
					lines[i] = line[:idx] + prefix + "[REDACTED]"
					if end < len(line) {
						lines[i] += line[end:]
					}
				}
			} else {
				end := min(len(line), idx+40)
				lines[i] = line[:idx] + "[REDACTED]"
				if end < len(line) {
					lines[i] += line[end:]
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// truncateStr truncates a string to max characters, appending "..." if needed.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
