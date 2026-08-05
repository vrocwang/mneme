package security

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AuditEventKind categorizes security-relevant events.
type AuditEventKind string

const (
	AuditToolExecution     AuditEventKind = "tool.execution"
	AuditToolBlocked       AuditEventKind = "tool.blocked"
	AuditCommandBlocked    AuditEventKind = "command.blocked"
	AuditPathViolation     AuditEventKind = "path.violation"
	AuditInjectionBlock    AuditEventKind = "injection.block"
	AuditApprovalPrompt    AuditEventKind = "approval.prompt"
	AuditApprovalDecide    AuditEventKind = "approval.decide"
	AuditSandboxEscalation AuditEventKind = "sandbox.escalation"
	AuditPairingAttempt    AuditEventKind = "pairing.attempt"
)

// AuditEvent is a security-relevant event recorded for review.
type AuditEvent struct {
	ID        string         `json:"id"`
	Kind      AuditEventKind `json:"kind"`
	ToolName  string         `json:"tool_name,omitempty"`
	Command   string         `json:"command,omitempty"`
	Args      string         `json:"args,omitempty"`
	Decision  string         `json:"decision,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// AuditLogger persists security audit events to SQLite.
type AuditLogger struct {
	db  *sql.DB
	log *slog.Logger
	mu  sync.RWMutex
}

// NewAuditLogger creates the audit table if it doesn't exist.
func NewAuditLogger(db *sql.DB, log *slog.Logger) (*AuditLogger, error) {
	if db == nil {
		return nil, fmt.Errorf("audit logger requires a database")
	}
	al := &AuditLogger{db: db, log: log}
	if err := al.migrate(); err != nil {
		return nil, fmt.Errorf("audit logger migration: %w", err)
	}
	return al, nil
}

func (al *AuditLogger) migrate() error {
	_, err := al.db.Exec(`
		CREATE TABLE IF NOT EXISTS security_audit (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			tool_name TEXT DEFAULT '',
			command TEXT DEFAULT '',
			args TEXT DEFAULT '',
			decision TEXT DEFAULT '',
			reason TEXT DEFAULT '',
			created_at TEXT NOT NULL
		)`)
	if err != nil {
		return err
	}
	_, err = al.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sec_audit_kind ON security_audit(kind)`)
	if err != nil {
		return err
	}
	_, err = al.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sec_audit_created ON security_audit(created_at DESC)`)
	return err
}

// Record writes an audit event.
func (al *AuditLogger) Record(kind AuditEventKind, opts AuditEvent) {
	al.mu.Lock()
	defer al.mu.Unlock()

	if opts.ID == "" {
		opts.ID = uuid.New().String()
	}
	opts.Kind = kind
	if opts.CreatedAt.IsZero() {
		opts.CreatedAt = time.Now().UTC()
	}

	_, err := al.db.Exec(
		`INSERT INTO security_audit (id, kind, tool_name, command, args, decision, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		opts.ID, string(kind), opts.ToolName, opts.Command, opts.Args,
		opts.Decision, opts.Reason, opts.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		al.log.Warn("failed to record audit event", "kind", kind, "error", err)
	}
}

// ListRecent returns the most recent N audit events.
func (al *AuditLogger) ListRecent(limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	al.mu.RLock()
	defer al.mu.RUnlock()

	rows, err := al.db.Query(
		`SELECT id, kind, tool_name, command, args, decision, reason, created_at
		 FROM security_audit ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var ca string
		if err := rows.Scan(&e.ID, &e.Kind, &e.ToolName, &e.Command, &e.Args, &e.Decision, &e.Reason, &ca); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		if e.CreatedAt.IsZero() {
			al.log.Warn("failed to parse audit event created_at, using current time", "id", e.ID, "created_at", ca)
			e.CreatedAt = time.Now().UTC()
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListByKind returns audit events filtered by kind.
func (al *AuditLogger) ListByKind(kind AuditEventKind, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	al.mu.RLock()
	defer al.mu.RUnlock()

	rows, err := al.db.Query(
		`SELECT id, kind, tool_name, command, args, decision, reason, created_at
		 FROM security_audit WHERE kind = ? ORDER BY created_at DESC LIMIT ?`, string(kind), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var ca string
		if err := rows.Scan(&e.ID, &e.Kind, &e.ToolName, &e.Command, &e.Args, &e.Decision, &e.Reason, &ca); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		if e.CreatedAt.IsZero() {
			al.log.Warn("failed to parse audit event created_at, using current time", "id", e.ID, "created_at", ca)
			e.CreatedAt = time.Now().UTC()
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
