package callbacks

import (
	"github.com/simon/mneme/internal/security"
)

// AuditCallback wraps a security.AuditLogger to record model and tool
// invocation events from the eino pipeline. All methods are nil-safe.
type AuditCallback struct {
	logger *security.AuditLogger
}

// NewAuditCallback creates an AuditCallback. Passing nil is allowed;
// methods will simply no-op.
func NewAuditCallback(logger *security.AuditLogger) *AuditCallback {
	return &AuditCallback{logger: logger}
}

// OnModelCall records an audit event when a model is invoked.
func (a *AuditCallback) OnModelCall(modelName, threadID string) {
	if a.logger == nil {
		return
	}
	a.logger.Record(
		security.AuditToolExecution,
		security.AuditEvent{
			ToolName: modelName,
			Args:     "thread=" + threadID,
			Reason:   "model call",
		},
	)
}

// OnToolCall records an audit event when a tool is invoked.
func (a *AuditCallback) OnToolCall(toolName, args, threadID string) {
	if a.logger == nil {
		return
	}
	a.logger.Record(
		security.AuditToolExecution,
		security.AuditEvent{
			ToolName: toolName,
			Args:     args,
			Reason:   "thread=" + threadID,
		},
	)
}
