package learning

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ToolStats records per-tool call statistics including success/failure counts,
// running average duration, and common error patterns.
type ToolStats struct {
	ToolName             string    `json:"tool_name"`
	TotalCalls           int64     `json:"total_calls"`
	Successes            int64     `json:"successes"`
	Failures             int64     `json:"failures"`
	AvgDurationMs        float64   `json:"avg_duration_ms"`
	CommonErrors         []string  `json:"common_errors,omitempty"` // capped at 5, FIFO eviction
	LastUpdated          time.Time `json:"last_updated"`
	totalDurationMsSoFar float64   // internal: running sum for average computation
}

// ToolStatsSnapshot is a read-only view of tool statistics.
type ToolStatsSnapshot struct {
	ToolName      string   `json:"tool_name"`
	TotalCalls    int64    `json:"total_calls"`
	SuccessRate   float64  `json:"success_rate"`
	AvgDurationMs float64  `json:"avg_duration_ms"`
	CommonErrors  []string `json:"common_errors,omitempty"`
}

// Snapshot returns a read-only view with computed success rate.
func (ts *ToolStats) Snapshot() ToolStatsSnapshot {
	rate := 0.0
	if ts.TotalCalls > 0 {
		rate = float64(ts.Successes) / float64(ts.TotalCalls)
	}
	return ToolStatsSnapshot{
		ToolName:      ts.ToolName,
		TotalCalls:    ts.TotalCalls,
		SuccessRate:   rate,
		AvgDurationMs: ts.AvgDurationMs,
		CommonErrors:  ts.CommonErrors,
	}
}

// ToolTrackerHook records per-tool call statistics after each agent turn.
// Implements the post-turn callback pattern used by ChatService.
type ToolTrackerHook struct {
	mu    sync.RWMutex
	stats map[string]*ToolStats // toolName → stats
	logFn func(msg string, args ...interface{})
}

// NewToolTrackerHook creates a tool tracking hook.
func NewToolTrackerHook(logFn func(msg string, args ...interface{})) *ToolTrackerHook {
	if logFn == nil {
		logFn = func(msg string, args ...interface{}) {}
	}
	return &ToolTrackerHook{
		stats: make(map[string]*ToolStats),
		logFn: logFn,
	}
}

// RecordCall records a single tool invocation result.
// errorSnippet is truncated to 80 characters.
func (h *ToolTrackerHook) RecordCall(toolName string, success bool, durationMs float64, errorSnippet string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ts, ok := h.stats[toolName]
	if !ok {
		ts = &ToolStats{ToolName: toolName}
		h.stats[toolName] = ts
	}

	ts.TotalCalls++
	ts.LastUpdated = time.Now()

	if success {
		ts.Successes++
	} else {
		ts.Failures++
		// Track unique error patterns (FIFO eviction at 5).
		if errorSnippet != "" {
			pattern := truncateStr(errorSnippet, 80)
			if !containsPattern(ts.CommonErrors, pattern) {
				if len(ts.CommonErrors) >= 5 {
					ts.CommonErrors = ts.CommonErrors[1:] // evict oldest
				}
				ts.CommonErrors = append(ts.CommonErrors, pattern)
			}
		}
	}

	// Running average: sum(durations) / n. Equivalent to Rust's
	// (old_avg * (n-1) + new_val) / n formula.
	ts.totalDurationMsSoFar += durationMs
	ts.AvgDurationMs = ts.totalDurationMsSoFar / float64(ts.TotalCalls)
}

// GetAllStats returns snapshots of all tracked tools.
func (h *ToolTrackerHook) GetAllStats() []ToolStatsSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	snapshots := make([]ToolStatsSnapshot, 0, len(h.stats))
	for _, ts := range h.stats {
		snapshots = append(snapshots, ts.Snapshot())
	}
	return snapshots
}

// GetStats returns snapshot for a specific tool, or nil if not tracked.
func (h *ToolTrackerHook) GetStats(toolName string) *ToolStatsSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if ts, ok := h.stats[toolName]; ok {
		s := ts.Snapshot()
		return &s
	}
	return nil
}

// PromptSection returns a markdown section describing tool effectiveness
// for injection into the system prompt. Returns empty string if no data.
func (h *ToolTrackerHook) PromptSection() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.stats) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Tool Effectiveness\n\n")
	b.WriteString("Recent tool call statistics. Prefer tools with higher success rates:\n\n")

	for _, ts := range h.stats {
		s := ts.Snapshot()
		status := "✅"
		if s.SuccessRate < 0.5 {
			status = "⚠️"
		} else if s.SuccessRate < 0.8 {
			status = "🔸"
		}
		b.WriteString(fmt.Sprintf("- %s **%s**: %d/%d (%.0f%%)",
			status, s.ToolName, ts.Successes, ts.TotalCalls, s.SuccessRate*100))
		if s.AvgDurationMs > 0 {
			b.WriteString(fmt.Sprintf(", avg %.0fms", s.AvgDurationMs))
		}
		b.WriteString("\n")
		if len(s.CommonErrors) > 0 {
			b.WriteString(fmt.Sprintf("  errors: %s\n", strings.Join(s.CommonErrors, "; ")))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// ── Post-turn integration ────────────────────────────────────────────────

// PostTurnCallback returns a function suitable for ChatService.AddTurnCallback.
// It inspects TurnResult.ToolCalls and records each tool invocation.
func (h *ToolTrackerHook) PostTurnCallback(ctx context.Context, result ToolTurnResult) error {
	for _, tc := range result.ToolCalls {
		durationMs := float64(tc.Duration.Milliseconds())
		errSnippet := ""
		if tc.Error != "" {
			errSnippet = tc.Error
		}
		h.RecordCall(tc.Name, tc.Success, durationMs, errSnippet)
	}
	return nil
}

// ToolTurnResult is a simplified view of agent.TurnResult for tool tracking.
type ToolTurnResult struct {
	ToolCalls []ToolCallRecord
}

// ToolCallRecord captures a single tool invocation.
type ToolCallRecord struct {
	Name     string
	Success  bool
	Error    string
	Duration time.Duration
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func containsPattern(patterns []string, p string) bool {
	for _, existing := range patterns {
		if existing == p {
			return true
		}
	}
	return false
}
