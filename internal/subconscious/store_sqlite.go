package subconscious

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStore provides persistent storage for tasks, reflections, and engine state
// using SQLite — matching the Rust subconscious store.rs pattern.
type SQLiteStore struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

// NewSQLiteStore opens or creates the SQLite database at the given workspace path.
func NewSQLiteStore(workspaceDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return nil, fmt.Errorf("create subconscious dir: %w", err)
	}
	dbPath := filepath.Join(workspaceDir, "subconscious.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open subconscious db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite serializes writes

	s := &SQLiteStore{db: db, path: dbPath}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate subconscious db: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		schedule TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		last_run_at TEXT NOT NULL DEFAULT '',
		next_run_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS reflections (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		payload TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		acted_on_at TEXT
	);
	CREATE TABLE IF NOT EXISTS engine_state (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS decision_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tick_number INTEGER NOT NULL,
		evaluator TEXT NOT NULL,
		decision TEXT NOT NULL,
		action_type TEXT NOT NULL DEFAULT '',
		action_title TEXT NOT NULL DEFAULT '',
		action_message TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_reflections_created ON reflections(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_decision_log_tick ON decision_log(tick_number);
	CREATE INDEX IF NOT EXISTS idx_tasks_next_run ON tasks(next_run_at);
	`
	_, err := s.db.Exec(schema)
	return err
}

// ── Task persistence ──────────────────────────────────────────────────────────

func (s *SQLiteStore) UpsertTask(task ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO tasks (id, name, schedule, enabled, last_run_at, next_run_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name, schedule=excluded.schedule, enabled=excluded.enabled,
		   last_run_at=excluded.last_run_at, next_run_at=excluded.next_run_at,
		   updated_at=datetime('now')`,
		task.ID, task.Name, task.Schedule, boolToInt(task.Enabled),
		task.LastRunAt.Format(time.RFC3339), task.NextRunAt.Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) ListTasks() ([]ScheduledTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT id, name, schedule, enabled, last_run_at, next_run_at FROM tasks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		var enabled int
		var lastRunStr, nextRunStr string
		if err := rows.Scan(&t.ID, &t.Name, &t.Schedule, &enabled, &lastRunStr, &nextRunStr); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		t.Enabled = enabled != 0
		t.LastRunAt, _ = time.Parse(time.RFC3339, lastRunStr)
		t.NextRunAt, _ = time.Parse(time.RFC3339, nextRunStr)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *SQLiteStore) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) MarkTaskRun(id string, ranAt time.Time, nextRunAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE tasks SET last_run_at = ?, next_run_at = ?, updated_at = datetime('now') WHERE id = ?`,
		ranAt.Format(time.RFC3339), nextRunAt.Format(time.RFC3339), id,
	)
	return err
}

// ── Reflection persistence ────────────────────────────────────────────────────

func (s *SQLiteStore) AddReflection(ref Reflection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref.ID == "" {
		ref.ID = fmt.Sprintf("ref-%d", time.Now().UnixNano())
	}
	if ref.CreatedAt.IsZero() {
		ref.CreatedAt = time.Now()
	}
	payload := jsonMarshalPayload(ref.Payload)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO reflections (id, kind, body, payload, created_at, acted_on_at) VALUES (?, ?, ?, ?, ?, ?)`,
		ref.ID, ref.Kind, ref.Body, payload, ref.CreatedAt.Format(time.RFC3339), nil,
	)
	return err
}

func (s *SQLiteStore) ListReflections(limit int) ([]Reflection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, kind, body, payload, created_at, acted_on_at FROM reflections ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Reflection
	for rows.Next() {
		var r Reflection
		var createdStr string
		var actedOnAt sql.NullString
		var payloadStr string
		if err := rows.Scan(&r.ID, &r.Kind, &r.Body, &payloadStr, &createdStr, &actedOnAt); err != nil {
			return nil, fmt.Errorf("scan reflection: %w", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		if actedOnAt.Valid {
			t, _ := time.Parse(time.RFC3339, actedOnAt.String)
			r.ActedOnAt = &t
		}
		if payloadStr != "" && payloadStr != "{}" {
			r.Payload = jsonUnmarshalPayload(payloadStr)
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

func (s *SQLiteStore) MarkReflectionActedOn(id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE reflections SET acted_on_at = ? WHERE id = ?`, at.Format(time.RFC3339), id)
	return err
}

func (s *SQLiteStore) CountReflections() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM reflections`).Scan(&count)
	return count, err
}

// ── Engine state persistence ──────────────────────────────────────────────────

func (s *SQLiteStore) GetEngineState(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var value string
	err := s.db.QueryRow(`SELECT value FROM engine_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *SQLiteStore) SetEngineState(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO engine_state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// ── Decision log ──────────────────────────────────────────────────────────────

func (s *SQLiteStore) LogDecision(tickNumber int64, evaluator, decision, actionType, actionTitle, actionMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO decision_log (tick_number, evaluator, decision, action_type, action_title, action_message)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		tickNumber, evaluator, decision, actionType, actionTitle, actionMessage,
	)
	return err
}

func (s *SQLiteStore) ListDecisionLog(limit int) ([]DecisionLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, tick_number, evaluator, decision, action_type, action_title, action_message, created_at
		 FROM decision_log ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DecisionLogEntry
	for rows.Next() {
		var e DecisionLogEntry
		if err := rows.Scan(&e.ID, &e.TickNumber, &e.Evaluator, &e.Decision, &e.ActionType, &e.ActionTitle, &e.ActionMessage, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan decision log: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// DecisionLogEntry is a recorded decision from the subconscious engine.
type DecisionLogEntry struct {
	ID            int64  `json:"id"`
	TickNumber    int64  `json:"tick_number"`
	Evaluator     string `json:"evaluator"`
	Decision      string `json:"decision"`
	ActionType    string `json:"action_type"`
	ActionTitle   string `json:"action_title"`
	ActionMessage string `json:"action_message"`
	CreatedAt     string `json:"created_at"`
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func jsonMarshalPayload(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func jsonUnmarshalPayload(data string) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return nil
	}
	return m
}
