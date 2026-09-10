package agent

import (
	"context"
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

// StreamCallback is called for each stream event.
type StreamCallback func(evt StreamEvent)

// HookContext carries metadata available to post-turn hooks.
type HookContext struct {
	ThreadID  string
	AgentName string
	AgentID   string
	AgentTier AgentTier
	Model     string
}

// TurnEndHook is called after a turn completes. AgentExperience.CaptureHook
// implements this to persist agent behavior patterns.
type TurnEndHook func(ctx context.Context, hc HookContext, result *TurnResult)

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

// ── Tool policy / tier types (kept for agent definitions) ──

// AgentToolPolicy defines per-tool access control for an agent.
type AgentToolPolicy struct {
	RequireApprovalFor []string `json:"requireApprovalFor,omitempty"`
	DenyTools          []string `json:"denyTools,omitempty"`
	MaxToolRounds      int      `json:"maxToolRounds,omitempty"`
}

// AgentTier labels the spawn hierarchy (chat > reasoning > worker).
type AgentTier string

const (
	TierChat      AgentTier = "chat"
	TierReasoning AgentTier = "reasoning"
	TierWorker    AgentTier = "worker"
)

