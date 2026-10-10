package monitor

import (
	"context"
	"log/slog"
	"time"
)

// RPC exposes monitor operations for the frontend. Wails RPC methods
// must not take context.Context — Wails injects its own lifecycle.
//
// The manager is resolved through a getter rather than captured at
// construction time: Wails evaluates the Bind list before OnStartup runs, so a
// captured pointer would be nil for the whole process lifetime.
type RPC struct {
	mgrFn func() *Manager
}

// DefaultRunTimeout is the monitor run timeout applied when the caller does not
// specify one. It is the single source of truth for both entry points: the
// monitor_start tool (internal/monitor/tools.go) and the RPC (StartRun here),
// which previously wrote 300*time.Second and 5*time.Minute respectively.
const DefaultRunTimeout = 5 * time.Minute

// runTimeout resolves a requested timeout in seconds, falling back to
// DefaultRunTimeout for a zero or negative request.
func runTimeout(secs int) time.Duration {
	if secs <= 0 {
		return DefaultRunTimeout
	}
	return time.Duration(secs) * time.Second
}

// NewRPC creates a monitor RPC handler. mgrFn is evaluated per call.
func NewRPC(mgrFn func() *Manager) *RPC {
	return &RPC{mgrFn: mgrFn}
}

// mgr resolves the monitor manager lazily.
func (r *RPC) mgr() *Manager {
	if r.mgrFn == nil {
		return nil
	}
	return r.mgrFn()
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
	m := r.mgr()
	if m == nil {
		return &ListRunsResult{}
	}
	m.RestoreFromDB()

	runs := m.List()
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
	m := r.mgr()
	if m == nil {
		return &StartRunResult{}
	}
	timeout := runTimeout(req.Timeout)
	ctx := context.Background()
	id, err := m.Start(ctx, req.Command, timeout, req.UsePTY)
	if err != nil {
		slog.Warn("monitor start failed", "command", req.Command, "error", err)
		return &StartRunResult{RunID: ""}
	}
	return &StartRunResult{RunID: id}
}

// GetRun reads a single run by ID.
func (r *RPC) GetRun(runID string) *RunSummary {
	m := r.mgr()
	if m == nil {
		return nil
	}
	run, err := m.Get(runID)
	if err != nil {
		slog.Warn("monitor get failed", "run", runID, "error", err)
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
	m := r.mgr()
	if m == nil {
		return ""
	}
	output, err := m.ReadOutput(runID)
	if err != nil {
		slog.Warn("monitor read output failed", "run", runID, "error", err)
		return ""
	}
	return output
}

// StopRun cancels a running monitor.
func (r *RPC) StopRun(runID string) {
	m := r.mgr()
	if m == nil {
		return
	}
	if err := m.Stop(runID); err != nil {
		slog.Warn("monitor stop failed", "run", runID, "error", err)
	}
}
