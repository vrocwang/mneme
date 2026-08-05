package security

import (
	"sync"
	"time"
)

// ActionTracker enforces rate limits on tool actions using a sliding window.
// It tracks the number of actions within a configurable time window and
// rejects requests when the limit is exceeded.
type ActionTracker struct {
	mu         sync.Mutex
	window     time.Duration
	maxActions int

	// Per-key tracking: key → list of action timestamps within the window.
	actions     map[string][]time.Time
	cleanupTick int // periodic GC counter (cleaned under mu)
}

// NewActionTracker creates a rate limiter with the given window and max actions.
func NewActionTracker(window time.Duration, maxActions int) *ActionTracker {
	return &ActionTracker{
		window:     window,
		maxActions: maxActions,
		actions:    make(map[string][]time.Time),
	}
}

// DefaultActionTracker returns a tracker with sensible defaults:
// 100 actions per 1-hour sliding window.
func DefaultActionTracker() *ActionTracker {
	return NewActionTracker(1*time.Hour, 100)
}

// RecordAction records an action for the given key. Returns true if the action
// is within limits, false if the rate limit has been exceeded.
func (t *ActionTracker) RecordAction(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	// Prune old entries. Clean up the key if no entries remain to prevent
	// unbounded map growth from transient keys.
	filtered := make([]time.Time, 0, len(t.actions[key]))
	for _, ts := range t.actions[key] {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}

	// Check limit.
	if len(filtered) >= t.maxActions {
		t.actions[key] = filtered
		return false
	}

	filtered = append(filtered, now)
	t.actions[key] = filtered
	// Clean up any other keys whose entries have fully expired to prevent
	// unbounded map growth from transient keys that are never checked again.
	if t.cleanupTick++; t.cleanupTick%1000 == 0 {
		t.gcLocked()
	}
	return true
}

// Remaining returns how many actions are left in the window for the key.
func (t *ActionTracker) Remaining(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	count := 0
	for _, ts := range t.actions[key] {
		if ts.After(cutoff) {
			count++
		}
	}

	remaining := t.maxActions - count
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// Reset clears all tracked actions. Useful for testing or policy changes.
func (t *ActionTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.actions = make(map[string][]time.Time)
}

// ResetKey clears actions for a specific key.
func (t *ActionTracker) ResetKey(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.actions, key)
}

// gcLocked removes keys whose entries have all expired. Called periodically
// from RecordAction under the mutex every ~1000 records.
func (t *ActionTracker) gcLocked() {
	now := time.Now()
	cutoff := now.Add(-t.window)
	for key, times := range t.actions {
		kept := times[:0]
		for _, ts := range times {
			if ts.After(cutoff) {
				kept = append(kept, ts)
			}
		}
		if len(kept) == 0 {
			delete(t.actions, key)
		} else if len(kept) < len(times) {
			t.actions[key] = kept
		}
	}
}

// Stats returns the current tracking state for monitoring.
func (t *ActionTracker) Stats(key string) (count int, limit int, remaining int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	for _, ts := range t.actions[key] {
		if ts.After(cutoff) {
			count++
		}
	}

	remaining = t.maxActions - count
	if remaining < 0 {
		remaining = 0
	}
	return count, t.maxActions, remaining
}

// ── ToolActionTracker ─────────────────────────────────────────────────────

// ToolActionTracker wraps ActionTracker with tool-scoped rate limiting.
type ToolActionTracker struct {
	tracker       *ActionTracker
	perToolLimits map[string]int // tool name → max actions per window
}

// NewToolActionTracker creates a tool-scoped rate limiter.
func NewToolActionTracker(window time.Duration, globalMax int) *ToolActionTracker {
	return &ToolActionTracker{
		tracker:       NewActionTracker(window, globalMax),
		perToolLimits: make(map[string]int),
	}
}

// SetPerToolLimit sets a per-tool action limit. 0 removes the limit.
func (t *ToolActionTracker) SetPerToolLimit(toolName string, max int) {
	t.tracker.mu.Lock()
	defer t.tracker.mu.Unlock()
	if max <= 0 {
		delete(t.perToolLimits, toolName)
	} else {
		t.perToolLimits[toolName] = max
	}
}

// CheckAndRecord validates both global and per-tool limits for a tool action
// and atomically records the action if within limits.
// Returns true if the action is allowed (and recorded), false if blocked.
func (t *ToolActionTracker) CheckAndRecord(threadID, toolName string) bool {
	t.tracker.mu.Lock()
	defer t.tracker.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.tracker.window)

	// Check and update global limit atomically.
	globalCount := t.countWithinWindow(threadID, cutoff)
	if globalCount >= t.tracker.maxActions {
		return false
	}

	// Check and update per-tool limit atomically.
	toolKey := threadID + ":" + toolName
	if limit, ok := t.perToolLimits[toolName]; ok {
		toolCount := t.countWithinWindow(toolKey, cutoff)
		if toolCount >= limit {
			return false
		}
		// Prune + record tool key.
		t.pruneAndRecord(toolKey, cutoff, now)
	}

	// Prune + record global key.
	t.pruneAndRecord(threadID, cutoff, now)
	return true
}

// countWithinWindow returns the number of actions for key within the cutoff time.
// Caller must hold t.tracker.mu.
func (t *ToolActionTracker) countWithinWindow(key string, cutoff time.Time) int {
	count := 0
	for _, ts := range t.tracker.actions[key] {
		if ts.After(cutoff) {
			count++
		}
	}
	return count
}

// pruneAndRecord prunes old entries and appends a new timestamp for key.
// Caller must hold t.tracker.mu.
func (t *ToolActionTracker) pruneAndRecord(key string, cutoff, now time.Time) {
	filtered := make([]time.Time, 0, len(t.tracker.actions[key]))
	for _, ts := range t.tracker.actions[key] {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	filtered = append(filtered, now)
	t.tracker.actions[key] = filtered
}
