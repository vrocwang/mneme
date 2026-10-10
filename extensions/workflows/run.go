// Workflow run lifecycle: spawn, await, complete, cancel. Ported from
// internal/workflows/run.go with two deliberate changes:
//
//  1. A per-run cancel flag was ADDED so workflow_cancel can release an
//     in-flight await_workflow (the host cannot cancel an extension call, so
//     the core's ctx-driven cancellation is not available). Cancel() sets the
//     flag, cancels the run context, and marks the record cancelled under the
//     lock; Await() polls the record every 200ms and therefore returns within
//     one tick. The completion path refuses to overwrite the cancelled status.
//
//  2. Run IDs are generated locally as UUID-v4 text (crypto/rand) instead of
//     github.com/google/uuid, so this module needs no dependency the core does
//     not already vendor for it. The string shape is identical.
//
// Runs live only in this process's map: they do NOT survive an extension
// restart. A restart loses every run record, the log files under
// <workspace>/workflow-logs remain on disk but are no longer addressable by
// run_id (list_workflow_runs and read_workflow_run_log can only see runs of the
// current process). This matches the core, which also kept them in memory.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RunStatus is the lifecycle state of a workflow run.
type RunStatus string

const (
	StatusPending   RunStatus = "pending"
	StatusRunning   RunStatus = "running"
	StatusCompleted RunStatus = "completed"
	StatusFailed    RunStatus = "failed"
	StatusCancelled RunStatus = "cancelled"
)

// RunRecord captures a single workflow execution.
type RunRecord struct {
	RunID      string    `json:"run_id"`
	WorkflowID string    `json:"workflow_id"`
	Status     RunStatus `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	LogPath    string    `json:"log_path"`
	Error      string    `json:"error,omitempty"`
}

// RunRequest is the input for spawning a workflow run.
type RunRequest struct {
	WorkflowID string            `json:"workflow_id"`
	Inputs     map[string]string `json:"inputs,omitempty"`
	Timeout    time.Duration     `json:"-"`
}

// RunFunc is the callback that executes the actual workflow logic.
type RunFunc func(ctx context.Context, req RunRequest, logWriter func(string)) (string, error)

// Runner manages workflow execution: spawn, await, cancel.
type Runner struct {
	mu      sync.Mutex
	runs    map[string]*activeRun
	logsDir string
}

type activeRun struct {
	record RunRecord
	cancel context.CancelFunc
	logs   *os.File
	// cancelled is set by Cancel and read/cleared only under Runner.mu. It is
	// the per-run cancel flag that makes the eventual RunFunc result unable to
	// flip a cancelled record back to completed.
	cancelled bool
}

// NewRunner creates a workflow runner. Logs are written under logsDir.
func NewRunner(logsDir string) (*Runner, error) {
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("workflow runner: create logs dir: %w", err)
	}
	return &Runner{
		runs:    make(map[string]*activeRun),
		logsDir: logsDir,
	}, nil
}

// Spawn starts a workflow run in the background and returns the run ID.
func (r *Runner) Spawn(req RunRequest, fn RunFunc) (string, error) {
	runID := newRunID()
	if req.Timeout == 0 {
		req.Timeout = 5 * time.Minute
	}

	logPath := filepath.Join(r.logsDir, runID+".log")
	f, err := os.Create(logPath)
	if err != nil {
		return "", fmt.Errorf("spawn workflow: create log: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), req.Timeout)
	ar := &activeRun{
		record: RunRecord{
			RunID:      runID,
			WorkflowID: req.WorkflowID,
			Status:     StatusRunning,
			StartedAt:  time.Now(),
			LogPath:    logPath,
		},
		cancel: cancel,
		logs:   f,
	}

	r.mu.Lock()
	r.runs[runID] = ar
	r.mu.Unlock()

	go func() {
		defer f.Close()

		logWriter := func(line string) {
			fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
			f.Sync()
		}
		logWriter(fmt.Sprintf("WORKFLOW_START workflow=%s", req.WorkflowID))

		output, runErr := fn(ctx, req, logWriter)

		// Lock-protected record update for safe concurrent reads.
		r.mu.Lock()
		ar.record.FinishedAt = time.Now()
		ar.record.DurationMs = ar.record.FinishedAt.Sub(ar.record.StartedAt).Milliseconds()
		if ar.cancelled {
			// Cancel already published StatusCancelled (and wrote the
			// WORKFLOW_CANCELLED log line); the RunFunc's context-derived
			// return value must not overwrite it.
			ar.record.Status = StatusCancelled
		} else if runErr != nil {
			ar.record.Status = StatusFailed
			ar.record.Error = runErr.Error()
		} else {
			ar.record.Status = StatusCompleted
		}
		r.mu.Unlock()

		if ar.cancelled {
			return
		}
		if runErr != nil {
			logWriter(fmt.Sprintf("WORKFLOW_FAILED error=%s", runErr.Error()))
		} else {
			logWriter(fmt.Sprintf("WORKFLOW_COMPLETED output_chars=%d", len(output)))
		}
	}()

	return runID, nil
}

// Await blocks until the workflow run completes or the context is cancelled.
func (r *Runner) Await(ctx context.Context, runID string) (RunRecord, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		r.mu.Lock()
		ar, ok := r.runs[runID]
		if !ok {
			r.mu.Unlock()
			return RunRecord{}, fmt.Errorf("await: run %q not found", runID)
		}
		status := ar.record.Status
		rec := ar.record
		r.mu.Unlock()

		if status != StatusRunning && status != StatusPending {
			return rec, nil
		}
		select {
		case <-ctx.Done():
			return rec, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Cancel stops a running workflow by flipping its per-run cancel flag. This is
// the extension-side replacement for the host's missing call cancellation: an
// await_workflow blocked on this run observes StatusCancelled within one 200ms
// poll and returns promptly, while the RunFunc goroutine is released through
// the run's context.
func (r *Runner) Cancel(runID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ar, ok := r.runs[runID]
	if !ok {
		return fmt.Errorf("cancel: run %q not found", runID)
	}
	if ar.record.Status != StatusRunning {
		return fmt.Errorf("cancel: run %q is already %s", runID, ar.record.Status)
	}
	ar.cancelled = true
	ar.cancel()
	ar.record.Status = StatusCancelled
	ar.record.FinishedAt = time.Now()
	ar.record.DurationMs = ar.record.FinishedAt.Sub(ar.record.StartedAt).Milliseconds()
	fmt.Fprintf(ar.logs, "%s WORKFLOW_CANCELLED\n", time.Now().Format(time.RFC3339))
	// Do NOT close ar.logs here — the goroutine's deferred f.Close()
	// owns file lifecycle. Writing to a closed file would lose the
	// goroutine's final WORKFLOW_COMPLETED / WORKFLOW_FAILED entry.
	return nil
}

// Complete marks a workflow run as successfully completed, cancelling its
// context so the runFunc goroutine exits and updates the record.
func (r *Runner) Complete(runID string, output string) error {
	r.mu.Lock()
	ar, ok := r.runs[runID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}
	fmt.Fprintf(ar.logs, "%s WORKFLOW_COMPLETE output=%s\n", time.Now().Format(time.RFC3339), output)
	ar.cancel()
	return nil
}

// Status returns the current status of a workflow run.
func (r *Runner) Status(runID string) (RunRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ar, ok := r.runs[runID]
	if !ok {
		return RunRecord{}, false
	}
	return ar.record, true
}

// ListRecent returns recent workflow runs, newest first.
func (r *Runner) ListRecent(workflowID string, limit int) []RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	type entry struct {
		id string
		ar *activeRun
	}
	var entries []entry
	for id, ar := range r.runs {
		if workflowID != "" && ar.record.WorkflowID != workflowID {
			continue
		}
		entries = append(entries, entry{id, ar})
	}
	// Sort newest first by started time.
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].ar.record.StartedAt.Before(entries[j].ar.record.StartedAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	if limit > 0 && limit < len(entries) {
		entries = entries[:limit]
	}
	var result []RunRecord
	for _, e := range entries {
		result = append(result, e.ar.record)
	}
	return result
}

// newRunID returns a random UUID-v4 string, matching the format the core got
// from github.com/google/uuid.
func newRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
