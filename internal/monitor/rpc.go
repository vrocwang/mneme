package monitor

import (
	"context"
	"database/sql"
	"time"
)

// RPC exposes monitor operations for the frontend. Wails RPC methods
// must not take context.Context — Wails injects its own lifecycle.
type RPC struct {
	mgr *Manager
}

// NewRPC creates a monitor RPC handler.
func NewRPC(mgr *Manager) *RPC {
	return &RPC{mgr: mgr}
}

// ListRunsResult is the JSON response for listing runs.
type ListRunsResult struct {
	Runs  []RunSummary `json:"runs"`
	Total int          `json:"total"`
}

// RunSummary is a lightweight view of a monitor run for the UI.
type RunSummary struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at,omitempty"`
}

// ListRuns returns all runs, restoring from DB first.
func (r *RPC) ListRuns() *ListRunsResult {
	if r.mgr == nil {
		return &ListRunsResult{}
	}
	r.mgr.RestoreFromDB()

	runs := r.mgr.List()
	summaries := make([]RunSummary, 0, len(runs))
	for _, run := range runs {
		var endedAt int64
		if !run.EndedAt.IsZero() {
			endedAt = run.EndedAt.UnixMilli()
		}
		summaries = append(summaries, RunSummary{
			ID:        run.ID,
			Command:   run.Command,
			Status:    string(run.Status),
			ExitCode:  run.ExitCode,
			Error:     run.Error,
			StartedAt: run.StartedAt.UnixMilli(),
			EndedAt:   endedAt,
		})
	}
	return &ListRunsResult{Runs: summaries, Total: len(summaries)}
}

// StartRunRequest is the body for starting a new monitored command.
type StartRunRequest struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout_secs"`
	UsePTY  bool   `json:"use_pty"`
}

// StartRunResult is the response after starting a run.
type StartRunResult struct {
	RunID string `json:"run_id"`
}

// StartRun begins a new monitored command.
func (r *RPC) StartRun(req StartRunRequest) *StartRunResult {
	if r.mgr == nil {
		return &StartRunResult{}
	}
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx := context.Background()
	id, err := r.mgr.Start(ctx, req.Command, timeout, req.UsePTY)
	if err != nil {
		return &StartRunResult{RunID: ""}
	}
	return &StartRunResult{RunID: id}
}

// GetRun reads a single run by ID.
func (r *RPC) GetRun(runID string) *RunSummary {
	if r.mgr == nil {
		return nil
	}
	run, err := r.mgr.Get(runID)
	if err != nil {
		return nil
	}
	var endedAt int64
	if !run.EndedAt.IsZero() {
		endedAt = run.EndedAt.UnixMilli()
	}
	return &RunSummary{
		ID:        run.ID,
		Command:   run.Command,
		Status:    string(run.Status),
		ExitCode:  run.ExitCode,
		Error:     run.Error,
		StartedAt: run.StartedAt.UnixMilli(),
		EndedAt:   endedAt,
	}
}

// ReadOutput returns the combined stdout+stderr for a run.
func (r *RPC) ReadOutput(runID string) string {
	if r.mgr == nil {
		return ""
	}
	output, err := r.mgr.ReadOutput(runID)
	if err != nil {
		return ""
	}
	return output
}

// StopRun cancels a running monitor.
func (r *RPC) StopRun(runID string) {
	if r.mgr == nil {
		return
	}
	_ = r.mgr.Stop(runID)
}

// WithDB enables SQLite persistence on the underlying manager.
func (r *RPC) WithDB(db *sql.DB) *RPC {
	r.mgr.WithDB(db)
	return r
}
