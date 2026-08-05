package approval

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store persists pending approvals, audit entries, and allowlist entries in SQLite.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// NewStore creates the approval tables if they don't exist.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("approval store requires a database")
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("approval store migration: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS approval_pending (
			id TEXT PRIMARY KEY,
			tool_name TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS approval_audit (
			id TEXT PRIMARY KEY,
			tool_name TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '',
			decision TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			decided_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS approval_allowlist (
			tool_name TEXT PRIMARY KEY,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_approval_audit_decided ON approval_audit(decided_at DESC)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// ── Pending approvals ─────────────────────────────────────────────────────

func (s *Store) SavePending(req *PendingApproval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO approval_pending (id, tool_name, args, reason, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		req.ID, req.ToolName, req.Args, req.Reason,
		req.CreatedAt.Format(time.RFC3339), req.ExpiresAt.Format(time.RFC3339),
	)
	return err
}

func (s *Store) DeletePending(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM approval_pending WHERE id = ?`, id)
	return err
}

func (s *Store) ListPending() ([]PendingApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, tool_name, args, reason, created_at, expires_at FROM approval_pending ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingApproval
	for rows.Next() {
		var p PendingApproval
		var ca, ea string
		if err := rows.Scan(&p.ID, &p.ToolName, &p.Args, &p.Reason, &ca, &ea); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTimeLog(ca)
		p.ExpiresAt = parseTimeLog(ea)
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecoverStale returns pending approvals whose expiry has passed.
func (s *Store) RecoverStale(now time.Time) ([]PendingApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, tool_name, args, reason, created_at, expires_at FROM approval_pending WHERE expires_at < ?`, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingApproval
	for rows.Next() {
		var p PendingApproval
		var ca, ea string
		if err := rows.Scan(&p.ID, &p.ToolName, &p.Args, &p.Reason, &ca, &ea); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTimeLog(ca)
		p.ExpiresAt = parseTimeLog(ea)
		out = append(out, p)
	}
	// Delete stale rows
	if len(out) > 0 {
		s.db.Exec(`DELETE FROM approval_pending WHERE expires_at < ?`, now.Format(time.RFC3339))
	}
	return out, rows.Err()
}

// ── Audit log ─────────────────────────────────────────────────────────────

func (s *Store) RecordAudit(entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO approval_audit (id, tool_name, args, decision, reason, created_at, decided_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.ToolName, entry.Args, entry.Decision, entry.Reason,
		entry.CreatedAt.Format(time.RFC3339), entry.DecidedAt.Format(time.RFC3339),
	)
	return err
}

// ListRecentDecisions returns the most recent N audit entries.
func (s *Store) ListRecentDecisions(limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, tool_name, args, decision, reason, created_at, decided_at
		 FROM approval_audit ORDER BY decided_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ca, da string
		if err := rows.Scan(&e.ID, &e.ToolName, &e.Args, &e.Decision, &e.Reason, &ca, &da); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTimeLog(ca)
		e.DecidedAt = parseTimeLog(da)
		out = append(out, e)
	}
	return out, rows.Err()
}

// parseTimeLog wraps time.Parse with a warning log on failure.
func parseTimeLog(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		slog.Warn("failed to parse time from store", "value", s, "error", err)
	}
	return t
}

// GetDecision reads the decision for a previously-recorded audit entry.
// Returns nil when no entry exists for the given ID. Used for TTL race
// resolution: if a user approved the request just as the timeout fired,
// the audit row is already committed and we should honour it.
func (s *Store) GetDecision(id string) (*Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var decisionStr string
	err := s.db.QueryRow(
		`SELECT decision FROM approval_audit WHERE id = ?`, id,
	).Scan(&decisionStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var d Decision
	switch decisionStr {
	case "approved_once", "auto_approved":
		d = DecisionApproveOnce
	case "approved_always":
		d = DecisionApproveAlways
	case "denied":
		d = DecisionDeny
	default:
		return nil, fmt.Errorf("unknown decision %q", decisionStr)
	}
	return &d, nil
}

// ── Allowlist ──────────────────────────────────────────────────────────────

func (s *Store) AddToAllowlist(toolName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO approval_allowlist (tool_name, created_at) VALUES (?, ?)`,
		toolName, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) RemoveFromAllowlist(toolName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM approval_allowlist WHERE tool_name = ?`, toolName)
	return err
}

func (s *Store) IsAllowlisted(toolName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM approval_allowlist WHERE tool_name = ?`, toolName).Scan(&count); err != nil {
		slog.Warn("IsAllowlisted query failed, returning false", "tool", toolName, "error", err)
		return false
	}
	return count > 0
}

func (s *Store) ListAllowlist() ([]AllowlistEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT tool_name, created_at FROM approval_allowlist ORDER BY tool_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AllowlistEntry
	for rows.Next() {
		var e AllowlistEntry
		var ca string
		if err := rows.Scan(&e.ToolName, &ca); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── Helpers ────────────────────────────────────────────────────────────────

func newID() string {
	return uuid.New().String()
}
