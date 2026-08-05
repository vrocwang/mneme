// Package approval provides an async approval middleware for supervised-mode tool execution.
// It parks tool calls that need user consent, persists pending requests to SQLite so they
// survive restarts, and records an audit log of every decision.
package approval

import (
	"strings"
	"time"
)

// Decision is the user's response to an approval request.
type Decision int

const (
	DecisionDeny          Decision = iota // deny this once
	DecisionApproveOnce                   // approve this single execution
	DecisionApproveAlways                 // approve this tool going forward (adds to allowlist)
)

func (d Decision) String() string {
	switch d {
	case DecisionDeny:
		return "deny"
	case DecisionApproveOnce:
		return "approve_once"
	case DecisionApproveAlways:
		return "approve_always"
	default:
		return "unknown"
	}
}

// PendingApproval is a request waiting for user action.
type PendingApproval struct {
	ID        string    `json:"id"`
	ToolName  string    `json:"tool_name"`
	Args      string    `json:"args"`             // JSON-encoded arguments
	Reason    string    `json:"reason"`           // human-readable summary
	Origin    string    `json:"origin,omitempty"` // turn origin kind from agent.TurnOrigin
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`

	resultCh chan Decision `json:"-"` // internal; not persisted
}

// AuditEntry records a decided approval for later review.
type AuditEntry struct {
	ID        string    `json:"id"`
	ToolName  string    `json:"tool_name"`
	Args      string    `json:"args"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason,omitempty"`
	Origin    string    `json:"origin,omitempty"` // turn origin kind at time of decision
	CreatedAt time.Time `json:"created_at"`
	DecidedAt time.Time `json:"decided_at"`
}

// GateOutcome captures what happened when the gate was consulted for a tool call.
type GateOutcome struct {
	Allowed    bool   `json:"allowed"`
	Prompted   bool   `json:"prompted"`
	Reason     string `json:"reason,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
}

// AllowlistEntry records a permanently allowed tool (DecisionApproveAlways).
type AllowlistEntry struct {
	ToolName  string    `json:"tool_name"`
	CreatedAt time.Time `json:"created_at"`
}

// ApprovalChatContext carries the thread and client ID for routing an
// approval reply back to the correct conversation.
type ApprovalChatContext struct {
	ThreadID string `json:"thread_id"`
	ClientID string `json:"client_id"`
}

// ToolResolver looks up a tool's permission level and external-effect status from the
// tool registry. Implementations that don't have access to the registry should return
// ("", false) to trigger the conservative fallback heuristic.
type ToolResolver func(toolName string) (permissionLevel string, hasExternalEffect bool)

// ApprovalGateBootState describes the gate's configuration at startup.
type ApprovalGateBootState string

const (
	BootInstalled       ApprovalGateBootState = "installed"
	BootDisabledByEnv   ApprovalGateBootState = "disabled_by_env"
	BootOverrideIgnored ApprovalGateBootState = "override_ignored"
	BootHost            ApprovalGateBootState = "host"
)

// ExecutionOutcome records the terminal status of a tool execution
// after the approval gate allowed it through.
type ExecutionOutcome string

const (
	OutcomeSuccess ExecutionOutcome = "success"
	OutcomeFailure ExecutionOutcome = "failure"
	OutcomeAborted ExecutionOutcome = "aborted"
)

// ParseApprovalReply extracts a Decision from a user's chat reply.
// Recognizes "yes", "approve", "allow", "always", "no", "deny", "reject".
func ParseApprovalReply(reply string) (Decision, bool) {
	lower := strings.ToLower(strings.TrimSpace(reply))
	switch {
	case lower == "yes" || lower == "y" || lower == "approve" || lower == "allow" || lower == "ok":
		return DecisionApproveOnce, true
	case lower == "always" || lower == "allow always" || lower == "approve always":
		return DecisionApproveAlways, true
	case lower == "no" || lower == "n" || lower == "deny" || lower == "reject" || lower == "block":
		return DecisionDeny, true
	default:
		return DecisionDeny, false
	}
}
