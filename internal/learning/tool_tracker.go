package learning

import (
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
