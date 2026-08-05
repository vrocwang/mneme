package queue

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ── Types ──────────────────────────────────────────────────────────

// JobKind discriminates the type of work.
type JobKind string

const (
	KindExtractChunk    JobKind = "extract_chunk"
	KindAppendBuffer    JobKind = "append_buffer"
	KindSeal            JobKind = "seal"
	KindFlushStale      JobKind = "flush_stale"
	KindReembedBackfill JobKind = "reembed_backfill"
)

// JobStatus represents the lifecycle state of a job.
type JobStatus string

const (
	StatusReady     JobStatus = "ready"
	StatusRunning   JobStatus = "running"
	StatusDone      JobStatus = "done"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

func (s JobStatus) IsTerminal() bool {
	return s == StatusDone || s == StatusFailed || s == StatusCancelled
}

// JobOutcome signals how the queue should settle the row after a handler run.
type JobOutcome struct {
	// Done marks the job as complete (status → done).
	Done bool
	// Defer reschedules the job without burning a failure attempt.
	// Useful for transient rate-limit backoff.
	Defer       bool
	DeferUntil  time.Time
	DeferReason string
}

// Job is one row in the jobs table.
type Job struct {
	ID          string
	Kind        JobKind
	PayloadJSON string
	DedupeKey   *string
	Status      JobStatus
	Attempts    int
	MaxAttempts int
	AvailableAt time.Time
	LockedUntil *time.Time
	LastError   *string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// Handler processes a job and returns an outcome.
type Handler func(ctx context.Context, job Job) (JobOutcome, error)

// ── Queue ───────────────────────────────────────────────────────────

type Config struct {
	// DB is the SQLite connection (WAL mode recommended).
	DB *sql.DB
	// WorkerCount is the number of concurrent worker goroutines.
	WorkerCount int
	// PollInterval is the sleep between empty polls.
	PollInterval time.Duration
	// LockTTL is how long a worker holds a claim before the row is
	// considered stale and eligible for recovery.
	LockTTL time.Duration
	// DefaultMaxAttempts for jobs that don't specify one.
	DefaultMaxAttempts int
}

func DefaultConfig(db *sql.DB) Config {
	return Config{
		DB:                 db,
		WorkerCount:        3,
		PollInterval:       2 * time.Second,
		LockTTL:            5 * time.Minute,
		DefaultMaxAttempts: 3,
	}
}

type Queue struct {
	cfg      Config
	handlers map[JobKind]Handler
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// New creates a persistent job queue backed by SQLite. Callers must call
// Migrate before Start. Workers are started via Start.
func New(cfg Config) *Queue {
	// Ensure sensible defaults
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 3
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.LockTTL <= 0 {
		cfg.LockTTL = 5 * time.Minute
	}
	if cfg.DefaultMaxAttempts <= 0 {
		cfg.DefaultMaxAttempts = 3
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{
		cfg:      cfg,
		handlers: make(map[JobKind]Handler),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// RegisterHandler sets the handler for a job kind. Must be called before Start.
func (q *Queue) RegisterHandler(kind JobKind, h Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[kind] = h
}

// Start begins processing jobs with the configured number of workers.
func (q *Queue) Start() {
	for i := 0; i < q.cfg.WorkerCount; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}

	// Start a recovery goroutine for stale locks
	q.wg.Add(1)
	go q.staleRecoveryLoop()
}

// Stop gracefully shuts down all workers. In-flight handlers are allowed to
// finish (context cancelled).
func (q *Queue) Stop() {
	q.cancel()
	q.wg.Wait()
}

// ── Migration ──────────────────────────────────────────────────────

// Migrate creates the jobs table if it doesn't exist. Idempotent.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mem_jobs (
			id              TEXT PRIMARY KEY,
			kind            TEXT NOT NULL,
			payload_json    TEXT NOT NULL DEFAULT '{}',
			dedupe_key      TEXT UNIQUE,
			status          TEXT NOT NULL DEFAULT 'ready',
			attempts        INTEGER NOT NULL DEFAULT 0,
			max_attempts    INTEGER NOT NULL DEFAULT 3,
			available_at    INTEGER NOT NULL DEFAULT 0,
			locked_until    INTEGER,
			last_error      TEXT,
			created_at      INTEGER NOT NULL DEFAULT (unixepoch('subsec')),
			started_at      INTEGER,
			completed_at    INTEGER
		);

		CREATE INDEX IF NOT EXISTS idx_mem_jobs_status_available
			ON mem_jobs(status, available_at)
			WHERE status = 'ready';

		CREATE INDEX IF NOT EXISTS idx_mem_jobs_locked_until
			ON mem_jobs(locked_until)
			WHERE status = 'running';
	`)
	return err
}

// ── Enqueue ─────────────────────────────────────────────────────────

// Enqueue inserts a new job. If a job with the same dedupe_key is in-flight
// (ready or running), it is silently skipped and returns (false, nil).
// Uses a transaction for the dedupe-check + insert to prevent races.
func (q *Queue) Enqueue(kind JobKind, payloadJSON string, dedupeKey string, availableAt time.Time) (bool, error) {
	id := newJobID()
	availMs := availableAt.UnixMilli()
	if availableAt.IsZero() {
		availMs = time.Now().UnixMilli()
	}

	var dedupeKeyVal interface{}
	if dedupeKey != "" {
		dedupeKeyVal = dedupeKey
	}

	tx, err := q.cfg.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return false, fmt.Errorf("enqueue begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO mem_jobs (id, kind, payload_json, dedupe_key, available_at, max_attempts)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, string(kind), payloadJSON, dedupeKeyVal, availMs, q.cfg.DefaultMaxAttempts,
	)
	if err != nil {
		// Check if this is a UNIQUE constraint violation on dedupe_key.
		// If so, check whether the existing job is in-flight — if it is,
		// silently skip (duplicate suppression). If the existing job is
		// completed or failed, delete it and re-insert to allow a retry.
		if dedupeKey != "" && strings.Contains(err.Error(), "UNIQUE constraint") {
			var existingStatus string
			scanErr := tx.QueryRow(
				`SELECT status FROM mem_jobs WHERE dedupe_key = ?`, dedupeKey,
			).Scan(&existingStatus)
			if scanErr == nil && (existingStatus == "ready" || existingStatus == "running") {
				return false, nil // duplicate in-flight, skip
			}
			// Existing job is done/failed — clear it so we can re-insert.
			tx.Exec(`DELETE FROM mem_jobs WHERE dedupe_key = ?`, dedupeKey)
			_, err = tx.Exec(
				`INSERT INTO mem_jobs (id, kind, payload_json, dedupe_key, available_at, max_attempts)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				id, string(kind), payloadJSON, dedupeKeyVal, availMs, q.cfg.DefaultMaxAttempts,
			)
			if err != nil {
				return false, fmt.Errorf("enqueue insert after dedupe cleanup: %w", err)
			}
		} else {
			return false, fmt.Errorf("enqueue insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("enqueue commit: %w", err)
	}
	return true, nil
}

// ── Workers ─────────────────────────────────────────────────────────

func (q *Queue) worker(id int) {
	defer q.wg.Done()
	ticker := time.NewTicker(q.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			q.pollAndRun(id)
		}
	}
}

func (q *Queue) staleRecoveryLoop() {
	defer q.wg.Done()
	ticker := time.NewTicker(q.cfg.LockTTL / 2)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			if err := q.recoverStaleLocks(); err != nil {
				// Log but don't crash — recovery is best-effort
				_ = err
			}
		}
	}
}

// pollAndRun claims the next available job and runs its handler.
func (q *Queue) pollAndRun(workerID int) {
	tx, err := q.cfg.DB.BeginTx(q.ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	now := time.Now()
	lockUntil := now.Add(q.cfg.LockTTL)

	// Claim next ready job (skip dedupe conflicts by not locking them)
	var job Job
	var availMs, lockedUntilMs, createdAtMs int64
	var startedAtMs, completedAtMs sql.NullInt64
	var dedupeKey, lastError sql.NullString

	err = tx.QueryRowContext(q.ctx,
		`SELECT id, kind, payload_json, dedupe_key, status, attempts, max_attempts,
		        available_at, locked_until, last_error, created_at, started_at, completed_at
		 FROM mem_jobs
		 WHERE status = 'ready' AND available_at <= ?
		 ORDER BY available_at ASC
		 LIMIT 1`,
		now.UnixMilli(),
	).Scan(
		&job.ID, &job.Kind, &job.PayloadJSON, &dedupeKey,
		&job.Status, &job.Attempts, &job.MaxAttempts,
		&availMs, &lockedUntilMs, &lastError, &createdAtMs,
		&startedAtMs, &completedAtMs,
	)
	if err == sql.ErrNoRows {
		return
	}
	if err != nil {
		return
	}

	// Lock the row
	lockUntilVal := lockUntil.UnixMilli()
	result, err := tx.ExecContext(q.ctx,
		`UPDATE mem_jobs SET status = 'running', locked_until = ?, started_at = ?, attempts = attempts + 1
		 WHERE id = ? AND status = 'ready'`,
		lockUntilVal, now.UnixMilli(), job.ID,
	)
	if err != nil {
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		// Another worker already claimed this job — rollback and skip.
		tx.Rollback()
		return
	}

	if err := tx.Commit(); err != nil {
		return
	}

	// Populate job fields
	job.Kind = JobKind(job.Kind)
	job.Status = StatusRunning
	if dedupeKey.Valid {
		job.DedupeKey = &dedupeKey.String
	}
	if lastError.Valid {
		job.LastError = &lastError.String
	}
	job.AvailableAt = time.UnixMilli(availMs)
	if startedAtMs.Valid {
		t := time.UnixMilli(startedAtMs.Int64)
		job.StartedAt = &t
	}
	if completedAtMs.Valid {
		t := time.UnixMilli(completedAtMs.Int64)
		job.CompletedAt = &t
	}
	job.CreatedAt = time.UnixMilli(createdAtMs)
	job.LockedUntil = &lockUntil

	// Find handler
	q.mu.RLock()
	handler, ok := q.handlers[job.Kind]
	q.mu.RUnlock()
	if !ok {
		q.settle(job.ID, StatusFailed, fmt.Errorf("no handler for kind %q", job.Kind))
		return
	}

	// Execute
	outcome, err := handler(q.ctx, job)
	if err != nil {
		q.settle(job.ID, StatusFailed, err)
		return
	}

	if outcome.Done {
		q.markDone(job.ID)
		return
	}

	if outcome.Defer {
		untilMs := outcome.DeferUntil.UnixMilli()
		reason := outcome.DeferReason
		if reason == "" {
			reason = "deferred by handler"
		}
		_, dbErr := q.cfg.DB.ExecContext(context.Background(),
			`UPDATE mem_jobs SET status = 'ready', locked_until = NULL, available_at = ?,
			 last_error = ?, attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END
			 WHERE id = ?`,
			untilMs, reason, job.ID,
		)
		if dbErr != nil {
			_ = dbErr
		}
		return
	}
}

// settle marks a job as failed, or retries if attempts remain.
// Uses a transaction to atomically check attempts and update status,
// preventing double-retry races between workers.
func (q *Queue) settle(jobID string, status JobStatus, runErr error) {
	errMsg := runErr.Error()
	ctx := context.Background()

	if status == StatusFailed {
		// Atomically check attempts and reschedule with backoff.
		tx, txErr := q.cfg.DB.BeginTx(ctx, nil)
		if txErr != nil {
			return
		}
		var attempts, maxAttempts int
		scanErr := tx.QueryRowContext(ctx,
			`SELECT attempts, max_attempts FROM mem_jobs WHERE id = ?`, jobID,
		).Scan(&attempts, &maxAttempts)
		if scanErr != nil || attempts >= maxAttempts {
			tx.Rollback()
			// Mark permanently failed.
			nowMs := time.Now().UnixMilli()
			q.cfg.DB.ExecContext(ctx,
				`UPDATE mem_jobs SET status = ?, locked_until = NULL,
				 completed_at = ?, last_error = ?
				 WHERE id = ?`,
				string(StatusFailed), nowMs, errMsg, jobID,
			)
			return
		}
		// attempts is post-increment from the DB UPDATE, so it starts at 1.
		// Use 1<<(attempts-1) so the first retry waits 1s, then 2s, 4s, ...
		backoff := time.Duration(minInt(1<<uint(attempts-1), 300)) * time.Second
		availableAt := time.Now().Add(backoff)
		_, txErr = tx.ExecContext(ctx,
			`UPDATE mem_jobs SET status = 'ready', locked_until = NULL,
			 available_at = ?, last_error = ?
			 WHERE id = ?`,
			availableAt.UnixMilli(), errMsg, jobID,
		)
		if txErr != nil {
			tx.Rollback()
			return
		}
		tx.Commit()
		return
	}

	nowMs := time.Now().UnixMilli()
	_, dbErr := q.cfg.DB.ExecContext(context.Background(),
		`UPDATE mem_jobs SET status = ?, locked_until = NULL,
		 completed_at = ?, last_error = ?
		 WHERE id = ?`,
		string(status), nowMs, errMsg, jobID,
	)
	if dbErr != nil {
		_ = dbErr
	}
}

func (q *Queue) markDone(jobID string) {
	nowMs := time.Now().UnixMilli()
	_, err := q.cfg.DB.ExecContext(context.Background(),
		`UPDATE mem_jobs SET status = 'done', locked_until = NULL, completed_at = ? WHERE id = ?`,
		nowMs, jobID,
	)
	if err != nil {
		_ = err
	}
}

// ShouldRetry returns true if the job has not yet reached its max attempts.
// Useful for status checks and monitoring.
func (q *Queue) ShouldRetry(jobID string) bool {
	var attempts, maxAttempts int
	err := q.cfg.DB.QueryRowContext(context.Background(),
		`SELECT attempts, max_attempts FROM mem_jobs WHERE id = ?`, jobID,
	).Scan(&attempts, &maxAttempts)
	if err != nil {
		return false
	}
	return attempts < maxAttempts
}

// AttemptsForJob returns the number of execution attempts for a job.
// Returns 0 if the job is not found.
func (q *Queue) AttemptsForJob(jobID string) int {
	var attempts int
	err := q.cfg.DB.QueryRowContext(context.Background(),
		`SELECT attempts FROM mem_jobs WHERE id = ?`, jobID,
	).Scan(&attempts)
	if err != nil {
		return 0
	}
	return attempts
}

// ── Stale lock recovery ────────────────────────────────────────────

func (q *Queue) recoverStaleLocks() error {
	ctx := context.Background()
	now := time.Now().UnixMilli()
	_, err := q.cfg.DB.ExecContext(ctx,
		`UPDATE mem_jobs SET status = 'ready', locked_until = NULL, last_error = 'stale lock recovered'
		 WHERE status = 'running' AND locked_until IS NOT NULL AND locked_until < ?`,
		now,
	)
	return err
}

// ── Helpers ─────────────────────────────────────────────────────────

func newJobID() string {
	b := make([]byte, 8)
	if _, err := cryptorand.Read(b); err != nil {
		// Fallback to nanosecond time if crypto/rand fails (extremely rare).
		return fmt.Sprintf("job_%d", time.Now().UnixNano())
	}
	return "job_" + hex.EncodeToString(b)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
