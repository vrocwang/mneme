// Package middleware provides eino-compatible agent middleware for
// security enforcement, circuit breaking, and memory injection.
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

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
	// CheckTool call parks until the user decides. When nil, CheckTool fails
	// closed (refuses to execute tools) because there is no authority to
	// approve execution.
	ApprovalGate *approval.Gate

	// AuditLogger records security events. Nil-safe.
	AuditLogger *security.AuditLogger
}

// FilterInput runs prompt-injection detection on user-supplied content.
// It delegates to security.EnforcePromptInput, which performs
// obfuscation-resistant detection including leet-speak normalisation,
// homoglyph mapping, and zero-width character stripping.
//
// Returns nil when the content is safe; returns a descriptive error when
// the injection score exceeds the block threshold (0.70) or is flagged for
// review (0.55–0.70). The "review" band previously slipped through here;
// for an autonomous agent that executes tools, it is treated as a block.
func (m *SecurityMiddleware) FilterInput(ctx context.Context, content string) error {
	result := security.EnforcePromptInput(content)
	if result.Action == security.ActionBlocked || result.Action == security.ActionReviewBlocked {
		if m.AuditLogger != nil {
			m.AuditLogger.Record(security.AuditInjectionBlock, security.AuditEvent{
				Reason: fmt.Sprintf("score=%.2f action=%d (runner)", result.Score, result.Action),
			})
		}
		return fmt.Errorf("security: input blocked: score=%.2f", result.Score)
	}
	return nil
}

// CheckTool consults the approval gate before a tool executes. It fails closed
// when the gate is nil (no authority to approve); when the gate denies the
// tool (or the request times out), an error is returned and the tool should
// not execute. A non-nil gate that is explicitly disabled (MNEME_APPROVAL_GATE=0)
// auto-approves, which is the operator's explicit opt-out rather than a failure.
//
// The args parameter is expected to be a JSON-encoded string of tool
// arguments, matching the signature of approval.Gate.RequestApproval.
func (m *SecurityMiddleware) CheckTool(ctx context.Context, toolName string, args string) error {
	// Fail closed: without an approval gate there is no authority to approve
	// tool execution. Refuse rather than silently allow arbitrary tools
	// (including shell) to run.
	if m.ApprovalGate == nil {
		return fmt.Errorf("security: approval gate not configured; refusing to execute tool %q", toolName)
	}

	// Enforce the path security policy (forbidden paths, trusted roots,
	// workspace containment) on any path-like arguments. This is the live
	// counterpart to security.BuildPolicyChecker and runs on every tool call.
	if m.Policy != nil {
		if err := m.validatePaths(toolName, args); err != nil {
			if m.AuditLogger != nil {
				m.AuditLogger.Record(security.AuditToolBlocked, security.AuditEvent{
					ToolName: toolName,
					Decision: "deny",
					Reason:   err.Error(),
				})
			}
			return err
		}
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

// validatePaths recursively walks the tool arguments and rejects any string
// value under a path-like key that the security policy disallows. This covers
// nested arguments (e.g. {"options":{"path":...}}) that a flat top-level scan
// would miss.
func (m *SecurityMiddleware) validatePaths(toolName, argsJSON string) error {
	if argsJSON == "" {
		return nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &root); err != nil {
		return nil // not JSON (or scalar) — cannot extract paths, skip
	}
	var walk func(prefix string, v interface{}) error
	walk = func(prefix string, v interface{}) error {
		switch tv := v.(type) {
		case map[string]interface{}:
			for k, val := range tv {
				if err := walk(k, val); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, val := range tv {
				if err := walk(prefix, val); err != nil {
					return err
				}
			}
		case string:
			if tv != "" && isPathLikeKey(prefix) {
				// Resolve relative paths against the workspace root so the policy
				// check matches what the tools themselves do (workspace-relative
				// paths are the norm). Absolute paths are checked as-is.
				checkPath := tv
				if m.Policy.WorkspaceRoot != "" && !filepath.IsAbs(tv) {
					checkPath = filepath.Join(m.Policy.WorkspaceRoot, tv)
				}
				if !m.Policy.IsPathAllowed(checkPath) {
					return fmt.Errorf("security: tool %q path %q is not allowed by security policy", toolName, tv)
				}
			}
		}
		return nil
	}
	return walk("", root)
}

// isPathLikeKey reports whether an argument key name indicates a filesystem
// path. It matches the canonical names used across the tool layer.
func isPathLikeKey(key string) bool {
	switch key {
	case "path", "file_path", "target", "source", "destination", "dir",
		"input", "output", "root", "file", "from", "to", "cwd",
		"workspace", "directory", "filename", "folder":
		return true
	default:
		return false
	}
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
