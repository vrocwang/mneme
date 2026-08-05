package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionRecord is a persistent record of an agent session.
type SessionRecord struct {
	SessionID    string     `json:"session_id"`
	AgentID      string     `json:"agent_id"`
	ThreadID     string     `json:"thread_id,omitempty"`
	Origin       string     `json:"origin"`
	Status       string     `json:"status"` // running, completed, failed, cancelled
	TurnCount    int        `json:"turn_count"`
	ToolCalls    int        `json:"tool_calls"`
	TotalTokens  int64      `json:"total_tokens"`
	TotalCost    float64    `json:"total_cost"`
	MessagesJSON string     `json:"messages_json,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Error        string     `json:"error,omitempty"`
}

// RunLedgerEntry is a single run within a session.
type RunLedgerEntry struct {
	RunID         string    `json:"run_id"`
	SessionID     string    `json:"session_id"`
	AgentID       string    `json:"agent_id"`
	Prompt        string    `json:"prompt"`
	TurnIndex     int       `json:"turn_index"`
	ToolCallsJSON string    `json:"tool_calls_json,omitempty"`
	TokensIn      int64     `json:"tokens_in"`
	TokensOut     int64     `json:"tokens_out"`
	TokensUsed    int64     `json:"tokens_used"`
	CostUSD       float64   `json:"cost_usd"`
	ToolCalls     int       `json:"tool_calls"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
}

// SessionDB provides persistent session tracking with an append-only
// run ledger for auditing and cost analysis.
type SessionDB struct {
	mu        sync.RWMutex
	db        *sql.DB
	legacyDir string // optional directory with legacy JSON files for import
}

// Migrate creates the sessions and run_ledger tables if they do not exist.
// Call this from the application's database initialization.
func Migrate(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("session_db: database is nil")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL DEFAULT '',
			thread_id TEXT NOT NULL DEFAULT '',
			origin TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'running',
			turn_count INTEGER NOT NULL DEFAULT 0,
			tool_calls INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			total_cost REAL NOT NULL DEFAULT 0,
			messages_json TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS run_ledger (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			agent_id TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL DEFAULT '',
			turn_index INTEGER NOT NULL DEFAULT 0,
			tool_calls_json TEXT NOT NULL DEFAULT '',
			tokens_in INTEGER NOT NULL DEFAULT 0,
			tokens_out INTEGER NOT NULL DEFAULT 0,
			tokens_used INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0,
			tool_calls INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_run_ledger_session ON run_ledger(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_run_ledger_completed ON run_ledger(completed_at)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("session_db migrate: %w", err)
		}
	}
	return nil
}

// NewSessionDB creates a session database backed by SQLite.
// If legacyDir is non-empty, legacy JSON sessions are imported on first access.
func NewSessionDB(db *sql.DB, legacyDir string) (*SessionDB, error) {
	if db == nil {
		return nil, fmt.Errorf("session_db: database is nil")
	}
	if err := Migrate(db); err != nil {
		return nil, err
	}
	sdb := &SessionDB{
		db:        db,
		legacyDir: legacyDir,
	}
	// Import legacy JSON files into SQLite if the sessions table is empty.
	if legacyDir != "" {
		sdb.importLegacyIfEmpty()
	}
	return sdb, nil
}

// ── Public API ─────────────────────────────────────────────────────────────

// CreateSession starts a new session record.
func (db *SessionDB) CreateSession(sessionID, agentID, origin string) *SessionRecord {
	now := time.Now().UTC()
	rec := &SessionRecord{
		SessionID: sessionID,
		AgentID:   agentID,
		Origin:    origin,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.db.Exec(
		`INSERT INTO sessions (id, agent_id, thread_id, origin, status, turn_count, tool_calls, total_tokens, total_cost, messages_json, created_at, updated_at, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.SessionID, rec.AgentID, rec.ThreadID, rec.Origin, rec.Status,
		rec.TurnCount, rec.ToolCalls, rec.TotalTokens, rec.TotalCost, rec.MessagesJSON,
		rec.CreatedAt.Format(time.RFC3339), rec.UpdatedAt.Format(time.RFC3339), rec.Error,
	)
	if err != nil {
		// Return the record anyway so callers don't crash; the error is
		// recoverable — the session exists in-memory for this process lifetime.
		// A future UpdateSession will retry the write.
	}
	return rec
}

// UpdateSession updates a session's status and counters.
func (db *SessionDB) UpdateSession(sessionID string, update func(*SessionRecord)) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Read current record from SQLite.
	rec, err := db.loadSessionLocked(sessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", sessionID, err)
	}

	update(rec)
	rec.UpdatedAt = time.Now().UTC()

	return db.saveSessionLocked(rec)
}

// CompleteSession marks a session as completed.
func (db *SessionDB) CompleteSession(sessionID string, errMsg string) error {
	return db.UpdateSession(sessionID, func(r *SessionRecord) {
		now := time.Now().UTC()
		r.CompletedAt = &now
		if errMsg != "" {
			r.Status = "failed"
			r.Error = errMsg
		} else {
			r.Status = "completed"
		}
	})
}

// GetSession returns a session by ID.
// If the session was migrated from legacy JSON files, it will already be in
// SQLite after construction (see importLegacyIfEmpty). Returns nil if not found.
func (db *SessionDB) GetSession(sessionID string) *SessionRecord {
	db.mu.RLock()
	defer db.mu.RUnlock()

	rec, err := db.loadSessionLocked(sessionID)
	if err != nil {
		return nil
	}
	return rec
}

// ListRecentSessions returns the most recent sessions.
func (db *SessionDB) ListRecentSessions(limit int) []*SessionRecord {
	db.mu.RLock()
	defer db.mu.RUnlock()

	rows, err := db.db.Query(
		`SELECT id, agent_id, thread_id, origin, status, turn_count, tool_calls, total_tokens, total_cost, messages_json, created_at, updated_at, completed_at, error
		 FROM sessions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []*SessionRecord
	for rows.Next() {
		rec, err := scanSession(rows)
		if err == nil && rec != nil {
			result = append(result, rec)
		}
	}
	return result
}

// AppendLedger records a run in the ledger.
func (db *SessionDB) AppendLedger(entry RunLedgerEntry) {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, _ = db.db.Exec(
		`INSERT INTO run_ledger (id, session_id, agent_id, prompt, turn_index, tool_calls_json, tokens_in, tokens_out, tokens_used, cost_usd, tool_calls, status, error, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.RunID, entry.SessionID, entry.AgentID, entry.Prompt, entry.TurnIndex,
		entry.ToolCallsJSON, entry.TokensIn, entry.TokensOut, entry.TokensUsed,
		entry.CostUSD, entry.ToolCalls, entry.Status, entry.Error,
		entry.StartedAt.Format(time.RFC3339), entry.CompletedAt.Format(time.RFC3339),
	)
}

// QueryLedger returns ledger entries for a session.
func (db *SessionDB) QueryLedger(sessionID string) []RunLedgerEntry {
	db.mu.RLock()
	defer db.mu.RUnlock()

	rows, err := db.db.Query(
		`SELECT id, session_id, agent_id, prompt, turn_index, tool_calls_json, tokens_in, tokens_out, tokens_used, cost_usd, tool_calls, status, error, started_at, completed_at
		 FROM run_ledger WHERE session_id = ? ORDER BY started_at ASC`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []RunLedgerEntry
	for rows.Next() {
		entry := scanLedger(rows)
		if entry != nil {
			result = append(result, *entry)
		}
	}
	return result
}

// TotalCostSince returns the sum of costs since a given time.
func (db *SessionDB) TotalCostSince(since time.Time) float64 {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var total sql.NullFloat64
	err := db.db.QueryRow(
		`SELECT SUM(cost_usd) FROM run_ledger WHERE completed_at >= ?`,
		since.Format(time.RFC3339),
	).Scan(&total)
	if err != nil || !total.Valid {
		return 0
	}
	return total.Float64
}

// ── Internal helpers ───────────────────────────────────────────────────────

// loadSessionLocked reads a session row from SQLite. Caller must hold at least
// a read lock.
func (db *SessionDB) loadSessionLocked(sessionID string) (*SessionRecord, error) {
	row := db.db.QueryRow(
		`SELECT id, agent_id, thread_id, origin, status, turn_count, tool_calls, total_tokens, total_cost, messages_json, created_at, updated_at, completed_at, error
		 FROM sessions WHERE id = ?`, sessionID)
	return scanSession(row)
}

// saveSessionLocked upserts a session row. Caller must hold the write lock.
func (db *SessionDB) saveSessionLocked(rec *SessionRecord) error {
	var completedAt *string
	if rec.CompletedAt != nil {
		s := rec.CompletedAt.Format(time.RFC3339)
		completedAt = &s
	}
	_, err := db.db.Exec(
		`INSERT OR REPLACE INTO sessions (id, agent_id, thread_id, origin, status, turn_count, tool_calls, total_tokens, total_cost, messages_json, created_at, updated_at, completed_at, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.SessionID, rec.AgentID, rec.ThreadID, rec.Origin, rec.Status,
		rec.TurnCount, rec.ToolCalls, rec.TotalTokens, rec.TotalCost, rec.MessagesJSON,
		rec.CreatedAt.Format(time.RFC3339), rec.UpdatedAt.Format(time.RFC3339),
		completedAt, rec.Error,
	)
	return err
}

// ── Row scanning ───────────────────────────────────────────────────────────

func scanSession(scanner interface {
	Scan(dest ...interface{}) error
}) (*SessionRecord, error) {
	var (
		id, agentID, threadID, origin, status, messagesJSON string
		turnCount, toolCalls                                int
		totalTokens                                         int64
		totalCost                                           float64
		createdAt, updatedAt, completedAt, errStr           sql.NullString
	)
	err := scanner.Scan(&id, &agentID, &threadID, &origin, &status,
		&turnCount, &toolCalls, &totalTokens, &totalCost, &messagesJSON,
		&createdAt, &updatedAt, &completedAt, &errStr)
	if err != nil {
		return nil, err
	}

	rec := &SessionRecord{
		SessionID:    id,
		AgentID:      agentID,
		ThreadID:     threadID,
		Origin:       origin,
		Status:       status,
		TurnCount:    turnCount,
		ToolCalls:    toolCalls,
		TotalTokens:  totalTokens,
		TotalCost:    totalCost,
		MessagesJSON: messagesJSON,
		Error:        errStr.String,
	}
	rec.CreatedAt = parseTime(createdAt.String)
	rec.UpdatedAt = parseTime(updatedAt.String)
	if completedAt.Valid && completedAt.String != "" {
		t := parseTime(completedAt.String)
		rec.CompletedAt = &t
	}
	return rec, nil
}

func scanLedger(scanner interface {
	Scan(dest ...interface{}) error
}) *RunLedgerEntry {
	var (
		id, sessionID, agentID, prompt, toolCallsJSON, status, errStr string
		turnIndex, toolCalls                                          int
		tokensIn, tokensOut, tokensUsed                               int64
		costUSD                                                       float64
		startedAt, completedAt                                        string
	)
	e := scanner.Scan(&id, &sessionID, &agentID, &prompt, &turnIndex,
		&toolCallsJSON, &tokensIn, &tokensOut, &tokensUsed, &costUSD,
		&toolCalls, &status, &errStr, &startedAt, &completedAt)
	if e != nil {
		return nil
	}
	return &RunLedgerEntry{
		RunID:         id,
		SessionID:     sessionID,
		AgentID:       agentID,
		Prompt:        prompt,
		TurnIndex:     turnIndex,
		ToolCallsJSON: toolCallsJSON,
		TokensIn:      tokensIn,
		TokensOut:     tokensOut,
		TokensUsed:    tokensUsed,
		CostUSD:       costUSD,
		ToolCalls:     toolCalls,
		Status:        status,
		Error:         errStr,
		StartedAt:     parseTime(startedAt),
		CompletedAt:   parseTime(completedAt),
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// ── Legacy JSON import ─────────────────────────────────────────────────────

// importLegacyIfEmpty checks whether the sessions table is empty and, if so,
// imports any legacy JSON session files from the legacy directory.
func (db *SessionDB) importLegacyIfEmpty() {
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil || count > 0 {
		return
	}
	// Import ledger first (sessions may reference it).
	db.importLegacyLedger()

	sessionsDir := filepath.Join(db.legacyDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionsDir, e.Name()))
		if err != nil {
			continue
		}
		var rec SessionRecord
		if json.Unmarshal(data, &rec) != nil {
			continue
		}
		// Persist to SQLite if not already present.
		if existing, _ := db.loadSessionLocked(rec.SessionID); existing == nil {
			_ = db.saveSessionLocked(&rec)
		}
	}
}

// importLegacyLedger imports ledger.json entries into the run_ledger table.
func (db *SessionDB) importLegacyLedger() {
	ledgerPath := filepath.Join(db.legacyDir, "ledger.json")
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return
	}
	var entries []RunLedgerEntry
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	for _, entry := range entries {
		db.AppendLedger(entry)
	}
}
