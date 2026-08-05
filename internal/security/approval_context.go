package security

import "context"

// approvalHandledKey is a context value signalling that the current tool
// invocation has already passed through the security middleware's approval
// gate. Tools that perform their own defense-in-depth gating (e.g. the shell
// tool's tier classification) use IsApprovalHandled to distinguish calls made
// via the tool-execution pipeline (approval already applied) from direct
// "raw registry" calls that bypass approval entirely.
type approvalHandledKey struct{}

// WithApprovalHandled returns a derived context marked as having passed the
// approval gate. The eino ToolWrapper sets this after SecurityMiddleware.CheckTool
// succeeds, so downstream tools know approval (when configured) was applied.
func WithApprovalHandled(ctx context.Context) context.Context {
	return context.WithValue(ctx, approvalHandledKey{}, true)
}

// IsApprovalHandled reports whether the context carries the approval-handled
// signal. When false, the call did not go through the approval pipeline and
// tools should conservatively block commands that would otherwise require
// interactive approval.
func IsApprovalHandled(ctx context.Context) bool {
	v, _ := ctx.Value(approvalHandledKey{}).(bool)
	return v
}
