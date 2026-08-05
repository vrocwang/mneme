package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Store provides SQLite-backed persistence for MCP audit entries.
// It works alongside the in-memory Log ring buffer — the Log is the hot
// cache for recent queries, and the Store provides durability and
// historical query capability.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// NewStore creates the MCP audit table if it doesn't exist.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("mcp audit store requires a database")
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("mcp audit migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS mcp_audit (
			id TEXT PRIMARY KEY,
			server_name TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			duration TEXT NOT NULL DEFAULT '',
			write_op INTEGER NOT NULL DEFAULT 0,
			timestamp TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_audit_server ON mcp_audit(server_name)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_audit_tool ON mcp_audit(tool_name)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_audit_ts ON mcp_audit(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_audit_write ON mcp_audit(write_op) WHERE write_op = 1`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// Save persists a single audit entry to SQLite.
func (s *Store) Save(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	argsJSON := "{}"
	if e.Args != nil {
		if b, err := json.Marshal(e.Args); err == nil {
			argsJSON = string(b)
		}
	}

	writeOp := 0
	if e.WriteOp {
		writeOp = 1
	}

	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO mcp_audit
		 (id, server_name, tool_name, args, result, error, duration, write_op, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Server, e.Tool, argsJSON, e.Result, e.Error, e.Duration,
		writeOp, e.Timestamp.Format(time.RFC3339),
	)
	return err
}

// List returns recent audit entries, newest first.
func (s *Store) List(limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, server_name, tool_name, args, result, error, duration, write_op, timestamp
		 FROM mcp_audit ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// ListByServer returns entries filtered by MCP server name.
func (s *Store) ListByServer(server string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, server_name, tool_name, args, result, error, duration, write_op, timestamp
		 FROM mcp_audit WHERE server_name = ? ORDER BY timestamp DESC LIMIT ?`, server, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// ListWriteOps returns only write-tool entries.
func (s *Store) ListWriteOps(limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, server_name, tool_name, args, result, error, duration, write_op, timestamp
		 FROM mcp_audit WHERE write_op = 1 ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// Stats returns summary statistics from the persistent store.
func (s *Store) Stats() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := map[string]interface{}{}

	var total, writeOps, errors int
	s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(write_op), 0) FROM mcp_audit`).Scan(&total, &writeOps)
	s.db.QueryRow(`SELECT COUNT(*) FROM mcp_audit WHERE error != ''`).Scan(&errors)

	stats["total"] = total
	stats["write_ops"] = writeOps
	stats["read_ops"] = total - writeOps
	stats["errors"] = errors

	rows, _ := s.db.Query(`SELECT server_name, COUNT(*) as cnt FROM mcp_audit GROUP BY server_name ORDER BY cnt DESC`)
	if rows != nil {
		defer rows.Close()
		servers := make(map[string]int)
		for rows.Next() {
			var name string
			var cnt int
			rows.Scan(&name, &cnt)
			servers[name] = cnt
		}
		stats["servers"] = servers
	}

	return stats
}

// PruneBefore deletes entries older than the given time. Returns the count
// of pruned entries.
func (s *Store) PruneBefore(before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM mcp_audit WHERE timestamp < ?`, before.Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		var e Entry
		var argsJSON string
		var writeOp int
		var ts string
		if err := rows.Scan(&e.ID, &e.Server, &e.Tool, &argsJSON, &e.Result,
			&e.Error, &e.Duration, &writeOp, &ts); err != nil {
			return nil, err
		}
		e.WriteOp = writeOp == 1
		e.Timestamp, _ = time.Parse(time.RFC3339, ts)
		if argsJSON != "" && argsJSON != "{}" {
			json.Unmarshal([]byte(argsJSON), &e.Args)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
