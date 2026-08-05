package cron

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// JobStore persists cron jobs and run history to SQLite.
type JobStore struct {
	db *sql.DB
	mu sync.Mutex
}

// RunRecord records a single cron job execution.
type RunRecord struct {
	ID     string    `json:"id"`
	JobID  string    `json:"job_id"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Error  string    `json:"error,omitempty"`
	Output string    `json:"output,omitempty"`
}

// NewJobStore creates the cron tables and returns a store. If db is nil,
// returns nil (no persistence).
func NewJobStore(db *sql.DB) (*JobStore, error) {
	if db == nil {
		return nil, nil
	}
	s := &JobStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("cron store migration: %w", err)
	}
	return s, nil
}

func (s *JobStore) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cron_jobs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			schedule TEXT NOT NULL,
			job_type TEXT NOT NULL DEFAULT 'builtin',
			agent_prompt TEXT NOT NULL DEFAULT '',
			shell_command TEXT NOT NULL DEFAULT '',
			delivery_channel TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_run TEXT NOT NULL DEFAULT '',
			next_run TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS cron_runs (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			start TEXT NOT NULL,
			end_t TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			output TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cron_runs_job ON cron_runs(job_id, start DESC)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// SaveJob persists a job. Creates or updates based on ID.
func (s *JobStore) SaveJob(job *Job) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO cron_jobs (id, name, schedule, job_type, agent_prompt,
		 shell_command, delivery_channel, enabled, last_run, next_run)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Name, job.Schedule, string(job.JobType), job.AgentPrompt,
		job.ShellCommand, job.DeliveryChannel, boolToInt(job.Enabled),
		timeOrEmpty(job.LastRun), timeOrEmpty(job.NextRun),
	)
	return err
}

// LoadJobs loads all persisted jobs into the scheduler.
func (s *JobStore) LoadJobs() ([]*Job, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, name, schedule, job_type, agent_prompt, shell_command,
		 delivery_channel, enabled, last_run, next_run
		 FROM cron_jobs ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var j Job
		var lastRun, nextRun string
		var enabled int
		if err := rows.Scan(&j.ID, &j.Name, &j.Schedule, &j.JobType, &j.AgentPrompt,
			&j.ShellCommand, &j.DeliveryChannel, &enabled, &lastRun, &nextRun); err != nil {
			return nil, err
		}
		j.Enabled = enabled != 0
		j.LastRun, _ = time.Parse(time.RFC3339, lastRun)
		j.NextRun, _ = time.Parse(time.RFC3339, nextRun)
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

// DeleteJob removes a persisted job.
func (s *JobStore) DeleteJob(id string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM cron_jobs WHERE id = ?`, id)
	return err
}

// SaveRun records a job execution result.
func (s *JobStore) SaveRun(record *RunRecord) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO cron_runs (id, job_id, start, end_t, error, output)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		record.ID, record.JobID, record.Start.Format(time.RFC3339),
		timeOrEmpty(record.End), record.Error, truncate(record.Output, 8000),
	)
	return err
}

// ListRuns returns the most recent runs for a job.
func (s *JobStore) ListRuns(jobID string, limit int) ([]RunRecord, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, job_id, start, end_t, error, output
		 FROM cron_runs WHERE job_id = ? ORDER BY start DESC LIMIT ?`,
		jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []RunRecord
	for rows.Next() {
		var r RunRecord
		var start, endT string
		if err := rows.Scan(&r.ID, &r.JobID, &start, &endT, &r.Error, &r.Output); err != nil {
			return nil, err
		}
		r.Start, _ = time.Parse(time.RFC3339, start)
		r.End, _ = time.Parse(time.RFC3339, endT)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// AllRuns returns all runs ordered by time descending (capped at 200).
func (s *JobStore) AllRuns() ([]RunRecord, error) {
	return s.ListRuns("", 200)
}

// ListAllRuns returns all runs across all jobs (capped at 200).
func (s *JobStore) ListAllRuns(limit int) ([]RunRecord, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, job_id, start, end_t, error, output
		 FROM cron_runs ORDER BY start DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []RunRecord
	for rows.Next() {
		var r RunRecord
		var start, endT string
		if err := rows.Scan(&r.ID, &r.JobID, &start, &endT, &r.Error, &r.Output); err != nil {
			return nil, err
		}
		r.Start, _ = time.Parse(time.RFC3339, start)
		r.End, _ = time.Parse(time.RFC3339, endT)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ── helpers ──

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func timeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
