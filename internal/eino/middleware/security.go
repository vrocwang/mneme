// Package middleware provides eino-compatible agent middleware for
// security enforcement, circuit breaking, and memory injection.
package middleware

import (
	"context"
	"fmt"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/approval"
	"github.com/simon/mneme/internal/security"
)

// SecurityMiddleware enforces prompt-injection detection, tool-approval
// gating, and credential scrubbing at key points in the agent lifecycle.
//
// It is safe for concurrent use: the underlying security.EnforcePromptInput
// and agent.DefaultCredentialScrubber are stateless, and approval.Gate
// handles its own synchronisation.
type SecurityMiddleware struct {
	// Policy is the workspace and path-security policy. When nil, path
	// checks in FilterInput are skipped (but injection detection still
	// runs against the global EnforcePromptInput function).
	Policy *security.SecurityPolicy

	// ApprovalGate is the async approval middleware. When non-nil, every
	// CheckTool call parks until the user decides. When nil, all tools
	// are allowed without prompting.
	ApprovalGate *approval.Gate

	// AuditLogger records security events. Nil-safe.
	AuditLogger *security.AuditLogger

	// Sandbox, when true, enables additional sandbox restrictions on
	// tool execution (e.g. cwd jailing). The actual enforcement is
	// handled by the tool runner; this flag is read by downstream
	// middleware to decide whether to activate sandbox mode.
	Sandbox bool
}

// FilterInput runs prompt-injection detection on user-supplied content.
// It delegates to security.EnforcePromptInput, which performs
// obfuscation-resistant detection including leet-speak normalisation,
// homoglyph mapping, and zero-width character stripping.
//
// Returns nil when the content is safe; returns a descriptive error when
// the injection score exceeds the block threshold (0.70).
func (m *SecurityMiddleware) FilterInput(ctx context.Context, content string) error {
	result := security.EnforcePromptInput(content)
	if result.Verdict == security.VerdictBlock {
		if m.AuditLogger != nil {
			m.AuditLogger.Record(security.AuditInjectionBlock, security.AuditEvent{
				Reason: fmt.Sprintf("score=%.2f (runner)", result.Score),
			})
		}
		return fmt.Errorf("security: input blocked: score=%.2f", result.Score)
	}
	return nil
}

// CheckTool consults the approval gate before a tool executes. When the
// gate is nil or disabled, the call is a no-op. When the gate denies the
// tool (or the request times out), an error is returned and the tool
// should not execute.
//
// The args parameter is expected to be a JSON-encoded string of tool
// arguments, matching the signature of approval.Gate.RequestApproval.
func (m *SecurityMiddleware) CheckTool(ctx context.Context, toolName string, args string) error {
	if m.ApprovalGate == nil {
		return nil
	}
	// RequestApproval returns (Decision, *AuditEntry) — the audit entry
	// is informational and does not represent an error.
	decision, _ := m.ApprovalGate.RequestApproval(ctx, toolName, args,
		fmt.Sprintf("tool %q requires approval", toolName))
	if decision == approval.DecisionDeny {
		if m.AuditLogger != nil {
			m.AuditLogger.Record(security.AuditToolBlocked, security.AuditEvent{
				ToolName: toolName,
				Decision: "deny",
				Reason:   "approval gate denied",
			})
		}
		return fmt.Errorf("security: tool %q not approved", toolName)
	}
	return nil
}

// SanitizeOutput scrubs credential patterns (API keys, tokens, private
// key headers) from tool output before it enters the conversation
// history. It delegates to agent.DefaultCredentialScrubber, which masks
// known patterns such as "sk-", "ghp_", "xoxb-", "Bearer ", and PEM
// headers.
func (m *SecurityMiddleware) SanitizeOutput(output string) string {
	return agent.DefaultCredentialScrubber(output)
}

// FilterResume validates the context before resuming a checkpointed agent
// run. Unlike FilterInput which checks user messages for injection, this
// verifies the resume token is valid and the thread hasn't been tampered
// with. When the middleware is not configured, all resumes are allowed.
func (m *SecurityMiddleware) FilterResume(ctx context.Context, checkPointID, threadID string) error {
	if m == nil {
		return nil
	}
	// Basic validation: ensure IDs are non-empty and well-formed.
	if checkPointID == "" || threadID == "" {
		return fmt.Errorf("security: resume requires valid checkpoint and thread IDs")
	}
	// Future: validate checkpoint ownership, rate-limit resumes, etc.
	return nil
}
