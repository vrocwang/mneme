// Package monitor provides background command execution with bounded output capture
// and status events. Monitors allow agents to start long-running commands and
// poll for status and output.
package monitor

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

const maxOutputBytes = 100 * 1024 // 100KB per monitor

// Status is the current state of a monitored command.
type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusTimeout   Status = "timeout"
)

// Run represents a single monitored command execution.
type Run struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	Status    Status    `json:"status"`
	ExitCode  int       `json:"exit_code"`
	Output    string    `json:"output"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Error     string    `json:"error,omitempty"`

	stdout bytes.Buffer
	stderr bytes.Buffer
	cancel context.CancelFunc
}

// Manager tracks active and completed monitor runs with optional SQLite persistence.
type Manager struct {
	mu      sync.RWMutex
	runs    map[string]*Run
	counter int64
	log     *slog.Logger
	db      *sql.DB
}

// NewManager creates a monitor manager. Pass nil for log to use default.
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		runs: make(map[string]*Run),
		log:  log.With("component", "monitor"),
	}
}

// WithDB enables SQLite persistence. Call before starting any runs.
func (m *Manager) WithDB(db *sql.DB) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db = db
	if db != nil {
		migrateMonitorDB(db)
	}
	return m
}

// migrateMonitorDB creates the monitor_runs table if needed.
func migrateMonitorDB(db *sql.DB) {
	db.Exec(`CREATE TABLE IF NOT EXISTS monitor_runs (
		id         TEXT PRIMARY KEY,
		command    TEXT NOT NULL,
		status     TEXT NOT NULL DEFAULT 'running',
		exit_code  INTEGER NOT NULL DEFAULT 0,
		output     TEXT NOT NULL DEFAULT '',
		error      TEXT NOT NULL DEFAULT '',
		started_at INTEGER NOT NULL,
		ended_at   INTEGER
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_runs_status ON monitor_runs(status)`)
}

// Start begins monitoring a command and returns the run ID.
func (m *Manager) Start(ctx context.Context, command string, timeout time.Duration, usePty bool) (string, error) {
	m.mu.Lock()
	m.counter++
	id := fmt.Sprintf("mon_%d", m.counter)
	run := &Run{
		ID:        id,
		Command:   command,
		Status:    StatusRunning,
		StartedAt: time.Now(),
	}
	m.runs[id] = run
	m.mu.Unlock()

	m.persistRun(run)

	execCtx, cancel := context.WithTimeout(context.Background(), timeout)
	run.cancel = cancel

	go m.runCommand(run, execCtx, command, usePty)

	return id, nil
}

func (m *Manager) runCommand(run *Run, execCtx context.Context, command string, usePty bool) {
	defer run.cancel()

	shellBin, shellFlag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shellBin, shellFlag = "cmd", "/c"
	}

	if usePty {
		m.runWithPTY(run, execCtx, shellBin, shellFlag, command)
	} else {
		m.runWithPipe(run, execCtx, shellBin, shellFlag, command)
	}
}

func (m *Manager) runWithPipe(run *Run, execCtx context.Context, shellBin, shellFlag, command string) {
	cmd := exec.CommandContext(execCtx, shellBin, shellFlag, command)
	cmd.Stdout = io.MultiWriter(&run.stdout, limitWriter{&run.stdout, maxOutputBytes})
	cmd.Stderr = io.MultiWriter(&run.stderr, limitWriter{&run.stderr, maxOutputBytes})

	err := cmd.Run()
	m.finishRun(run, execCtx, err)
}

func (m *Manager) runWithPTY(run *Run, execCtx context.Context, shellBin, shellFlag, command string) {
	cmd := exec.Command(shellBin, shellFlag, command)
	pty, err := startPTY(cmd)
	if err != nil {
		// PTY unavailable on this platform or system — fall back to pipe.
		m.runWithPipe(run, execCtx, shellBin, shellFlag, command)
		return
	}
	defer pty.cleanup()

	// Read PTY output into stdout buffer.
	readDone := make(chan struct{})
	go func() {
		io.Copy(io.MultiWriter(&run.stdout, limitWriter{&run.stdout, maxOutputBytes}), pty.out)
		close(readDone)
	}()

	// Wait for command completion or timeout.
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- pty.wait()
	}()

	var runErr error
	select {
	case runErr = <-waitDone:
	case <-execCtx.Done():
		pty.kill()
		runErr = <-waitDone
	}

	// Drain remaining PTY output.
	pty.out.Close()
	<-readDone

	m.finishRun(run, execCtx, runErr)
}

func (m *Manager) finishRun(run *Run, execCtx context.Context, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	run.EndedAt = time.Now()
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			run.Status = StatusTimeout
			run.Error = "timed out"
		} else {
			run.Status = StatusFailed
			run.ExitCode = 1
			run.Error = err.Error()
		}
	} else {
		run.Status = StatusCompleted
	}
	m.persistRun(run)
	m.log.Debug("monitor finished", "id", run.ID, "status", run.Status)
}

// Get returns a run by ID.
func (m *Manager) Get(id string) (*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	run, ok := m.runs[id]
	if !ok {
		return nil, fmt.Errorf("monitor %q not found", id)
	}
	return run, nil
}

// List returns all runs.
func (m *Manager) List() []*Run {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Run
	for _, r := range m.runs {
		out = append(out, r)
	}
	return out
}

// Stop cancels a running monitor.
func (m *Manager) Stop(id string) error {
	m.mu.RLock()
	run, ok := m.runs[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("monitor %q not found", id)
	}
	if run.cancel != nil {
		run.cancel()
	}
	return nil
}

// ReadOutput returns combined output for a run.
func (m *Manager) ReadOutput(id string) (string, error) {
	m.mu.RLock()
	run, ok := m.runs[id]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("monitor %q not found", id)
	}
	return run.stdout.String() + run.stderr.String(), nil
}

// limitWriter caps the total bytes written.
type limitWriter struct {
	buf *bytes.Buffer
	max int
}

func (w limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len() >= w.max {
		return len(p), nil // discard silently
	}
	remaining := w.max - w.buf.Len()
	if len(p) > remaining {
		return w.buf.Write(p[:remaining])
	}
	return w.buf.Write(p)
}

// ── SQLite persistence ─────────────────────────────────────────────

func (m *Manager) persistRun(run *Run) {
	if m.db == nil {
		return
	}
	endedAt := int64(0)
	if !run.EndedAt.IsZero() {
		endedAt = run.EndedAt.UnixMilli()
	}
	output := run.stdout.String() + run.stderr.String()
	_, err := m.db.Exec(
		`INSERT OR REPLACE INTO monitor_runs (id, command, status, exit_code, output, error, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.Command, string(run.Status), run.ExitCode, output, run.Error,
		run.StartedAt.UnixMilli(), endedAt,
	)
	if err != nil {
		m.log.Warn("monitor persist failed", "id", run.ID, "error", err)
	}
}

// RestoreFromDB loads previously persisted runs from SQLite into memory.
func (m *Manager) RestoreFromDB() (int, error) {
	if m.db == nil {
		return 0, nil
	}
	rows, err := m.db.Query(`SELECT id, command, status, exit_code, output, error, started_at, ended_at
		FROM monitor_runs ORDER BY started_at DESC LIMIT 500`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	var restored int
	for rows.Next() {
		var r Run
		var startedMs, endedMs int64
		var statusStr string
		if err := rows.Scan(&r.ID, &r.Command, &statusStr, &r.ExitCode, &r.Output, &r.Error,
			&startedMs, &endedMs); err != nil {
			continue
		}
		r.Status = Status(statusStr)
		r.StartedAt = time.UnixMilli(startedMs)
		if endedMs > 0 {
			r.EndedAt = time.UnixMilli(endedMs)
		}
		r.stdout.WriteString(r.Output)
		if _, exists := m.runs[r.ID]; !exists {
			m.runs[r.ID] = &r
			restored++
		}
	}
	return restored, rows.Err()
}
