package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// SkillRunStatus is the lifecycle state of a skill run.
type SkillRunStatus string

const (
	RunStatusPending   SkillRunStatus = "pending"
	RunStatusRunning   SkillRunStatus = "running"
	RunStatusCompleted SkillRunStatus = "completed"
	RunStatusFailed    SkillRunStatus = "failed"
	RunStatusCancelled SkillRunStatus = "cancelled"
)

// SkillRun represents a single execution of a skill.
type SkillRun struct {
	RunID       string                 `json:"run_id"`
	SkillName   string                 `json:"skill_name"`
	Status      SkillRunStatus         `json:"status"`
	Args        map[string]interface{} `json:"args,omitempty"`
	Output      string                 `json:"output,omitempty"`
	Error       string                 `json:"error,omitempty"`
	ExitCode    int                    `json:"exit_code"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	DurationMs  int64                  `json:"duration_ms"`
}

// RunLogManager persists skill run logs to disk and supports reading
// slices of log output for large runs.
type RunLogManager struct {
	mu      sync.RWMutex
	runsDir string
	runs    map[string]*SkillRun // in-memory index
}

// NewRunLogManager creates a run log manager.
func NewRunLogManager(runsDir string) (*RunLogManager, error) {
	os.MkdirAll(runsDir, 0700)
	m := &RunLogManager{
		runsDir: runsDir,
		runs:    make(map[string]*SkillRun),
	}
	m.scanRuns()
	return m, nil
}

// StartRun records the start of a skill execution.
func (m *RunLogManager) StartRun(skillName string, args map[string]interface{}) (*SkillRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	run := &SkillRun{
		RunID:     fmt.Sprintf("run_%d", time.Now().UnixNano()),
		SkillName: skillName,
		Status:    RunStatusRunning,
		Args:      args,
		StartedAt: time.Now().UTC(),
	}
	m.runs[run.RunID] = run
	m.persistRun(run)
	return run, nil
}

// CompleteRun marks a run as completed or failed.
func (m *RunLogManager) CompleteRun(runID string, output string, err error, exitCode int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	run, ok := m.runs[runID]
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}

	now := time.Now().UTC()
	run.CompletedAt = &now
	run.DurationMs = now.Sub(run.StartedAt).Milliseconds()
	run.ExitCode = exitCode

	if err != nil {
		run.Status = RunStatusFailed
		run.Error = err.Error()
	} else {
		run.Status = RunStatusCompleted
		run.Output = output
	}

	m.persistRun(run)
	return nil
}

// CancelRun marks a run as cancelled.
func (m *RunLogManager) CancelRun(runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	run, ok := m.runs[runID]
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}
	run.Status = RunStatusCancelled
	now := time.Now().UTC()
	run.CompletedAt = &now
	run.DurationMs = now.Sub(run.StartedAt).Milliseconds()
	m.persistRun(run)
	return nil
}

// RecentRuns returns the most recent runs.
func (m *RunLogManager) RecentRuns(limit int) []*SkillRun {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*SkillRun, 0, len(m.runs))
	for _, r := range m.runs {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.After(result[j].StartedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

// FindRunLogPath returns the path to a run's log file.
func (m *RunLogManager) FindRunLogPath(runID string) string {
	return filepath.Join(m.runsDir, runID+".json")
}

// ReadRunLogSlice reads a slice of a run's log output by offset and max bytes.
func (m *RunLogManager) ReadRunLogSlice(runID string, offset, maxBytes int64) (string, error) {
	m.mu.RLock()
	run, ok := m.runs[runID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("run %q not found", runID)
	}

	output := run.Output
	if offset >= int64(len(output)) {
		return "", nil
	}
	end := offset + maxBytes
	if end > int64(len(output)) {
		end = int64(len(output))
	}
	return output[offset:end], nil
}

// ── Persistence ─────────────────────────────────────────────────────

func (m *RunLogManager) persistRun(run *SkillRun) {
	data, _ := json.Marshal(run)
	os.WriteFile(m.FindRunLogPath(run.RunID), data, 0600)
}

func (m *RunLogManager) scanRuns() {
	entries, _ := os.ReadDir(m.runsDir)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.runsDir, e.Name()))
		if err != nil {
			continue
		}
		var run SkillRun
		if json.Unmarshal(data, &run) == nil {
			m.runs[run.RunID] = &run
		}
	}
}
