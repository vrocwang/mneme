package security

// AuditRPC exposes the security audit trail to the Wails frontend. Events are
// recorded throughout the security path (blocked commands, path violations,
// approval prompts/decisions, injection blocks, pairing attempts); without a
// reader the table was write-only. The logger is created during startup, after
// the Wails Bind list is evaluated, so it is resolved through a getter.
type AuditRPC struct {
	logger func() *AuditLogger
}

// NewAuditRPC creates an audit RPC handler. loggerFn is evaluated per call.
func NewAuditRPC(loggerFn func() *AuditLogger) *AuditRPC {
	return &AuditRPC{logger: loggerFn}
}

func (r *AuditRPC) resolve() *AuditLogger {
	if r == nil || r.logger == nil {
		return nil
	}
	return r.logger()
}

// ListAuditEvents returns the most recent audit events, newest first.
func (r *AuditRPC) ListAuditEvents(limit int) ([]AuditEvent, error) {
	al := r.resolve()
	if al == nil {
		return nil, nil
	}
	return al.ListRecent(limit)
}

// ListAuditEventsByKind returns audit events of one kind, newest first.
// An empty kind returns the most recent events across all kinds.
func (r *AuditRPC) ListAuditEventsByKind(kind string, limit int) ([]AuditEvent, error) {
	al := r.resolve()
	if al == nil {
		return nil, nil
	}
	if kind == "" {
		return al.ListRecent(limit)
	}
	return al.ListByKind(AuditEventKind(kind), limit)
}
