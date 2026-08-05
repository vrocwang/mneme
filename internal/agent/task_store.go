package agent

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// TaskStore persists DispatchTask records to SQLite, enabling task state
// to survive process restarts.
type TaskStore struct {
	db *sql.DB
	mu sync.Mutex
}

// NewTaskStore creates the dispatch_tasks table and returns a store.
func NewTaskStore(db *sql.DB) (*TaskStore, error) {
	if db == nil {
		return nil, nil
	}
	s := &TaskStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("task store migration: %w", err)
	}
	return s, nil
}

func (s *TaskStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS dispatch_tasks (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT 'normal',
			status TEXT NOT NULL DEFAULT 'pending',
			max_retries INTEGER NOT NULL DEFAULT 3,
			retry_count INTEGER NOT NULL DEFAULT 0,
			scheduled_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			started_at TEXT,
			completed_at TEXT,
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			last_heartbeat TEXT,
			session_thread_id TEXT NOT NULL DEFAULT '',
			claim_token TEXT NOT NULL DEFAULT '',
			claimed_at TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_dispatch_tasks_status
			ON dispatch_tasks(status, priority, created_at);
	`)
	return err
}

// Enqueue inserts a new task and sets its ID and CreatedAt.
func (s *TaskStore) Enqueue(task *DispatchTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if task.ID == "" {
		task.ID = fmt.Sprintf("task_%d", now.UnixNano())
	}
	task.CreatedAt = now
	task.Status = "pending"

	_, err := s.db.Exec(
		`INSERT INTO dispatch_tasks (id, agent_id, prompt, priority, status, max_retries, retry_count, scheduled_at, created_at, started_at, completed_at, result, error, last_heartbeat, session_thread_id, claim_token, claimed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.AgentID, task.Prompt, task.Priority, task.Status, task.MaxRetries, task.RetryCount,
		formatTime(task.ScheduledAt), formatTime(task.CreatedAt),
		nullableTime(task.StartedAt), nullableTime(task.CompletedAt),
		task.Result, task.Error, nullableTime(task.LastHeartbeat),
		task.SessionThreadID, task.ClaimToken, nullableTime(task.ClaimedAt),
	)
	if err != nil {
		return fmt.Errorf("task_store: enqueue: %w", err)
	}
	return nil
}

// Claim atomically acquires a pending task by setting it to running with the given claim token.
// Returns true if the claim succeeded (task was pending and is now claimed).
func (s *TaskStore) Claim(taskID, claimToken string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := formatTime(time.Now().UTC())
	result, err := s.db.Exec(
		`UPDATE dispatch_tasks SET status='running', claim_token=?, claimed_at=?, started_at=? WHERE id=? AND status='pending'`,
		claimToken, now, now, taskID,
	)
	if err != nil {
		return false, fmt.Errorf("task_store: claim: %w", err)
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

// Complete marks a task as completed with its result.
func (s *TaskStore) Complete(taskID, result string) error {
	return s.updateStatus(taskID, "completed", result, "")
}

// Fail marks a task as failed. If retry is true, the task is reset to pending
// and its retry_count is incremented.
func (s *TaskStore) Fail(taskID, errMsg string, retry bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if retry {
		_, err := s.db.Exec(
			`UPDATE dispatch_tasks SET status='pending', error=?, retry_count=retry_count+1, claim_token='', completed_at=? WHERE id=?`,
			errMsg, formatTime(time.Now().UTC()), taskID,
		)
		return err
	}
	return s.updateStatus(taskID, "failed", "", errMsg)
}

// Cancel marks a task as cancelled.
func (s *TaskStore) Cancel(taskID string) error {
	return s.updateStatus(taskID, "cancelled", "", "")
}

// Heartbeat updates the last_heartbeat timestamp for a running task.
func (s *TaskStore) Heartbeat(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`UPDATE dispatch_tasks SET last_heartbeat=? WHERE id=?`,
		formatTime(time.Now().UTC()), taskID,
	)
	return err
}

// ReleaseClaim returns a claimed task back to pending state.
func (s *TaskStore) ReleaseClaim(taskID, claimToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`UPDATE dispatch_tasks SET status='pending', claim_token='', claimed_at=NULL WHERE id=? AND claim_token=?`,
		taskID, claimToken,
	)
	return err
}

// List returns tasks filtered by status. An empty status returns all tasks.
func (s *TaskStore) List(status string) ([]*DispatchTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var rows *sql.Rows
	var err error
	if status == "" {
		rows, err = s.db.Query(`SELECT id, agent_id, prompt, priority, status, max_retries, retry_count, scheduled_at, created_at, started_at, completed_at, result, error, last_heartbeat, session_thread_id, claim_token, claimed_at FROM dispatch_tasks ORDER BY created_at DESC`)
	} else {
		rows, err = s.db.Query(`SELECT id, agent_id, prompt, priority, status, max_retries, retry_count, scheduled_at, created_at, started_at, completed_at, result, error, last_heartbeat, session_thread_id, claim_token, claimed_at FROM dispatch_tasks WHERE status=? ORDER BY priority, created_at ASC`, status)
	}
	if err != nil {
		return nil, fmt.Errorf("task_store: list: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// Get returns a single task by ID.
func (s *TaskStore) Get(taskID string) (*DispatchTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT id, agent_id, prompt, priority, status, max_retries, retry_count, scheduled_at, created_at, started_at, completed_at, result, error, last_heartbeat, session_thread_id, claim_token, claimed_at FROM dispatch_tasks WHERE id=?`, taskID)
	t := &DispatchTask{}
	var id string
	var scheduledAt, createdAt, startedAt, completedAt, lastHeartbeat, claimedAt sql.NullString
	err := row.Scan(&id, &t.AgentID, &t.Prompt, &t.Priority, &t.Status, &t.MaxRetries, &t.RetryCount, &scheduledAt, &createdAt, &startedAt, &completedAt, &t.Result, &t.Error, &lastHeartbeat, &t.SessionThreadID, &t.ClaimToken, &claimedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	t.ID = id
	t.ScheduledAt = parseTime(scheduledAt.String)
	t.CreatedAt = parseTime(createdAt.String)
	if startedAt.Valid {
		tm := parseTime(startedAt.String)
		t.StartedAt = &tm
	}
	if completedAt.Valid {
		tm := parseTime(completedAt.String)
		t.CompletedAt = &tm
	}
	if lastHeartbeat.Valid {
		tm := parseTime(lastHeartbeat.String)
		t.LastHeartbeat = &tm
	}
	if claimedAt.Valid {
		tm := parseTime(claimedAt.String)
		t.ClaimedAt = &tm
	}
	return t, err
}

// ReclaimStale finds tasks that are running but haven't had a heartbeat within
// the timeout, and resets them to pending. Returns the count of reclaimed tasks.
func (s *TaskStore) ReclaimStale(timeout time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := formatTime(time.Now().UTC().Add(-timeout))
	result, err := s.db.Exec(
		`UPDATE dispatch_tasks SET status='pending', claim_token='', claimed_at=NULL WHERE status='running' AND (last_heartbeat IS NULL OR last_heartbeat < ?)`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("task_store: reclaim_stale: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (s *TaskStore) updateStatus(taskID, status, result, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := formatTime(time.Now().UTC())
	_, err := s.db.Exec(
		`UPDATE dispatch_tasks SET status=?, result=?, error=?, completed_at=? WHERE id=?`,
		status, result, errMsg, now, taskID,
	)
	return err
}

// ── scan helpers ────────────────────────────────────────────────────────
// SELECT order: id, agent_id, prompt, priority, status, max_retries,
// retry_count, scheduled_at, created_at, started_at, completed_at, result,
// error, last_heartbeat, session_thread_id, claim_token, claimed_at

func scanTasks(rows *sql.Rows) ([]*DispatchTask, error) {
	var tasks []*DispatchTask
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func scanTaskRow(rows *sql.Rows) (*DispatchTask, error) {
	var t DispatchTask
	var id string
	var scheduledAt, createdAt, startedAt, completedAt, lastHeartbeat, claimedAt sql.NullString

	err := rows.Scan(&id, &t.AgentID, &t.Prompt, &t.Priority, &t.Status,
		&t.MaxRetries, &t.RetryCount,
		&scheduledAt, &createdAt, &startedAt, &completedAt,
		&t.Result, &t.Error,
		&lastHeartbeat, &t.SessionThreadID, &t.ClaimToken, &claimedAt)
	if err != nil {
		return nil, err
	}

	t.ID = id
	t.ScheduledAt = parseTime(scheduledAt.String)
	t.CreatedAt = parseTime(createdAt.String)
	if startedAt.Valid {
		tm := parseTime(startedAt.String)
		t.StartedAt = &tm
	}
	if completedAt.Valid {
		tm := parseTime(completedAt.String)
		t.CompletedAt = &tm
	}
	if lastHeartbeat.Valid {
		tm := parseTime(lastHeartbeat.String)
		t.LastHeartbeat = &tm
	}
	if claimedAt.Valid {
		tm := parseTime(claimedAt.String)
		t.ClaimedAt = &tm
	}
	return &t, nil
}

// ── time helpers ─────────────────────────────────────────────────────────

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func nullableTime(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
