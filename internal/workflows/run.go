package workflows

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/simon/mneme/internal/capability"
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
	runID := uuid.New().String()
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
		if runErr != nil {
			ar.record.Status = StatusFailed
			ar.record.Error = runErr.Error()
		} else {
			ar.record.Status = StatusCompleted
		}
		r.mu.Unlock()

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

// Cancel stops a running workflow.
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

// RunCount returns the total number of runs tracked.
func (r *Runner) RunCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

// RegisterAll creates the workflow runner and registers all workflow tools
// into the capability registry under the "workflows" set. Returns the runner
// for lifecycle management.
func RegisterAll(capReg *capability.CapabilityRegistry, workspaceRoot string, log *slog.Logger) (*Runner, error) {
	userSkillsDir := filepath.Join(workspaceRoot, "skills")
	wfLogsDir := filepath.Join(workspaceRoot, "workflow-logs")

	runner, err := NewRunner(wfLogsDir)
	if err != nil {
		log.Warn("workflow runner init failed", "error", err)
		return nil, err
	}

	capReg.EnsureSet(&capability.CapabilitySet{
		ID:      "workflows",
		Name:    "Workflows",
		Kind:    capability.KindBuiltin,
		Enabled: true,
	})

	capReg.RegisterTool("workflows", &ListWorkflowsTool{UserDir: userSkillsDir, ProjectDir: workspaceRoot})
	capReg.RegisterTool("workflows", &DescribeWorkflowTool{UserDir: userSkillsDir, ProjectDir: workspaceRoot})
	capReg.RegisterTool("workflows", &RunWorkflowTool{Runner: runner, UserDir: userSkillsDir, ProjectDir: workspaceRoot})
	capReg.RegisterTool("workflows", &CompleteWorkflowTool{Runner: runner})
	capReg.RegisterTool("workflows", &AwaitWorkflowTool{Runner: runner})
	capReg.RegisterTool("workflows", &ListWorkflowRunsTool{Runner: runner})
	capReg.RegisterTool("workflows", &ReadWorkflowRunLogTool{Runner: runner})

	// Register Wails RPC for frontend workflow install/uninstall.
	// Uses capability.RegisterWailsRPC to avoid import cycles.
	capability.RegisterWailsRPC(NewWorkflowRPC(userSkillsDir))

	log.Info("workflow system initialized")
	return runner, nil
}
