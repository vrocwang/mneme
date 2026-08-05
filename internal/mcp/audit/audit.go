// Package audit provides MCP write-tool audit logging for inspection
// and accountability of tool calls made through MCP servers.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Entry records a single MCP tool invocation.
type Entry struct {
	ID        string                 `json:"id"`
	Server    string                 `json:"server"`
	Tool      string                 `json:"tool"`
	Args      map[string]interface{} `json:"args"`
	Result    string                 `json:"result"` // truncated
	Error     string                 `json:"error,omitempty"`
	Duration  string                 `json:"duration"`
	Timestamp time.Time              `json:"timestamp"`
	WriteOp   bool                   `json:"write_op"` // true if this was a write tool
}

// Log is an in-memory audit log for MCP tool calls.
type Log struct {
	mu      sync.RWMutex
	entries []Entry
	maxSize int
}

// New creates a new MCP audit log.
func New(maxSize int) *Log {
	if maxSize <= 0 {
		maxSize = 5000
	}
	return &Log{
		entries: make([]Entry, 0),
		maxSize: maxSize,
	}
}

// Record adds an audit entry.
func (l *Log) Record(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e.Timestamp = time.Now()
	l.entries = append(l.entries, e)

	if len(l.entries) > l.maxSize {
		excess := len(l.entries) - l.maxSize
		l.entries = l.entries[excess:]
	}
}

// RecordToolCall is a convenience method that records a tool call and returns
// a function to finalize the entry with the result.
func (l *Log) RecordToolCall(server, tool string, args map[string]interface{}, writeOp bool) func(result, errStr string) {
	start := time.Now()
	id := fmt.Sprintf("%s-%s-%d", server, tool, start.UnixNano())

	return func(result, errStr string) {
		l.Record(Entry{
			ID:       id,
			Server:   server,
			Tool:     tool,
			Args:     args,
			Result:   truncateForLog(result, 500),
			Error:    errStr,
			Duration: time.Since(start).Round(time.Millisecond).String(),
			WriteOp:  writeOp,
		})
	}
}

// List returns recent audit entries, newest first.
func (l *Log) List(limit int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 || limit > len(l.entries) {
		limit = len(l.entries)
	}

	result := make([]Entry, limit)
	for i := 0; i < limit; i++ {
		result[i] = l.entries[len(l.entries)-1-i]
	}
	return result
}

// ListByServer returns entries filtered by MCP server name.
func (l *Log) ListByServer(server string, limit int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var matches []Entry
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].Server == server {
			matches = append(matches, l.entries[i])
			if limit > 0 && len(matches) >= limit {
				break
			}
		}
	}
	return matches
}

// ListWriteOps returns only write-tool entries.
func (l *Log) ListWriteOps(limit int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var matches []Entry
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].WriteOp {
			matches = append(matches, l.entries[i])
			if limit > 0 && len(matches) >= limit {
				break
			}
		}
	}
	return matches
}

// Stats returns summary statistics.
func (l *Log) Stats() map[string]interface{} {
	l.mu.RLock()
	defer l.mu.RUnlock()

	servers := make(map[string]int)
	tools := make(map[string]int)
	var writeOps, readOps, errors int

	for _, e := range l.entries {
		servers[e.Server]++
		tools[e.Tool]++
		if e.WriteOp {
			writeOps++
		} else {
			readOps++
		}
		if e.Error != "" {
			errors++
		}
	}

	return map[string]interface{}{
		"total":     len(l.entries),
		"write_ops": writeOps,
		"read_ops":  readOps,
		"errors":    errors,
		"servers":   servers,
		"tools":     tools,
	}
}

// FormatEntries returns a human-readable log of entries.
func FormatEntries(entries []Entry) string {
	if len(entries) == 0 {
		return "No audit entries found."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("MCP Audit Log (%d entries):\n\n", len(entries)))
	for _, e := range entries {
		opIcon := "[READ]"
		if e.WriteOp {
			opIcon = "[WRITE]"
		}
		status := "OK"
		if e.Error != "" {
			status = "ERROR: " + e.Error
		}
		b.WriteString(fmt.Sprintf("%s %s/%s %s — %s (%s)\n",
			opIcon, e.Server, e.Tool, status, e.Duration, e.Timestamp.Format("15:04:05")))
	}
	return b.String()
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// WriteToolNames is a set of tool name patterns that indicate write operations.
var WriteToolNames = map[string]bool{
	"write_file":         true,
	"edit_file":          true,
	"apply_patch":        true,
	"shell":              true,
	"git_operations":     true,
	"http_post":          true,
	"memory_save":        true,
	"credential_store":   true,
	"todo_add":           true,
	"todo_update_status": true,
	"todo_edit":          true,
	"todo_remove":        true,
	"browser":            true,
}

// IsWriteTool returns true if the tool name indicates a write operation.
func IsWriteTool(name string) bool {
	return WriteToolNames[name]
}

// ── SQLite-backed audit logger ───────────────────────────────────────

// AuditEntry records a single MCP tool execution in the SQLite audit log.
// (Renamed from "Entry" to avoid conflict with the in-memory Entry type.)
type AuditEntry struct {
	ID         int64     `json:"id"`
	Server     string    `json:"server"`
	Tool       string    `json:"tool"`
	Args       string    `json:"args"` // JSON-sanitized (first 500 chars)
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	Timestamp  time.Time `json:"timestamp"`
}

// Logger persists MCP tool execution audit entries.
type Logger struct {
	db  *sql.DB
	log *slog.Logger
}

// NewLogger creates an MCP audit logger backed by SQLite.
func NewLogger(db *sql.DB, log *slog.Logger) (*Logger, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if log == nil {
		log = slog.Default()
	}
	l := &Logger{db: db, log: log.With("component", "mcp-audit")}

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS mcp_audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server TEXT NOT NULL,
		tool TEXT NOT NULL,
		args TEXT DEFAULT '',
		success INTEGER NOT NULL DEFAULT 0,
		error TEXT DEFAULT '',
		duration_ms INTEGER NOT NULL DEFAULT 0,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return nil, fmt.Errorf("create mcp_audit_log table: %w", err)
	}
	return l, nil
}

// Log records an MCP tool execution.
func (l *Logger) Log(ctx context.Context, server, tool string, args map[string]interface{}, success bool, errMsg string, duration time.Duration) error {
	argsJSON := sanitizeArgs(args)
	_, dbErr := l.db.ExecContext(ctx,
		`INSERT INTO mcp_audit_log (server, tool, args, success, error, duration_ms, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		server, tool, argsJSON, boolToInt(success), errMsg, duration.Milliseconds(), time.Now(),
	)
	if dbErr != nil {
		l.log.Error("mcp audit log write failed", "server", server, "tool", tool, "error", dbErr)
		return dbErr
	}
	l.log.Debug("mcp audit logged", "server", server, "tool", tool, "success", success, "duration_ms", duration.Milliseconds())
	return nil
}

// Query returns recent audit entries, newest first.
func (l *Logger) Query(ctx context.Context, server string, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if server == "" {
		rows, err = l.db.QueryContext(ctx,
			`SELECT id, server, tool, args, success, error, duration_ms, timestamp
			 FROM mcp_audit_log ORDER BY timestamp DESC LIMIT ?`, limit)
	} else {
		rows, err = l.db.QueryContext(ctx,
			`SELECT id, server, tool, args, success, error, duration_ms, timestamp
			 FROM mcp_audit_log WHERE server = ? ORDER BY timestamp DESC LIMIT ?`, server, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Server, &e.Tool, &e.Args, &e.Success, &e.Error, &e.DurationMs, &e.Timestamp); err != nil {
			return entries, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Stats returns audit statistics for all or a specific server.
func (l *Logger) Stats(ctx context.Context, server string) (map[string]interface{}, error) {
	var total, success, failed int
	var err error
	if server == "" {
		err = l.db.QueryRowContext(ctx, `SELECT COUNT(*), SUM(success), COUNT(*) - SUM(success) FROM mcp_audit_log`).Scan(&total, &success, &failed)
	} else {
		err = l.db.QueryRowContext(ctx, `SELECT COUNT(*), SUM(success), COUNT(*) - SUM(success) FROM mcp_audit_log WHERE server = ?`, server).Scan(&total, &success, &failed)
	}
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total":   total,
		"success": success,
		"failed":  failed,
	}, nil
}

func sanitizeArgs(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}
	// Redact common sensitive keys.
	sanitized := make(map[string]interface{}, len(args))
	for k, v := range args {
		if isSensitive(k) {
			sanitized[k] = "[REDACTED]"
		} else {
			sanitized[k] = v
		}
	}
	b, _ := json.Marshal(sanitized)
	if len(b) > 500 {
		return string(b[:500]) + "..."
	}
	return string(b)
}

func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, sensitive := range []string{"token", "password", "secret", "key", "auth", "credential", "api_key"} {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
