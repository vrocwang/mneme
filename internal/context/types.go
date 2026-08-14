package context

import (
	"sync"
)

// SessionMemoryTracker tracks content accumulation since last memory extraction.
type SessionMemoryTracker struct {
	mu sync.Mutex

	tokenDelta    int
	toolCallCount int
	turnCount     int
	extracting    bool

	TurnsBetween   int // default 5
	TokenThreshold int // default 4000
	ToolThreshold  int // default 8
}

// NewSessionMemoryTracker creates a tracker with sensible defaults.
func NewSessionMemoryTracker() *SessionMemoryTracker {
	return &SessionMemoryTracker{
		TurnsBetween:   5,
		TokenThreshold: 4000,
		ToolThreshold:  8,
	}
}

// NoteTurn records a completed turn's metrics.
func (t *SessionMemoryTracker) NoteTurn(tokenDelta, toolCalls int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tokenDelta += tokenDelta
	t.toolCallCount += toolCalls
	t.turnCount++
}

// ShouldExtract returns true if enough content has accumulated.
func (t *SessionMemoryTracker) ShouldExtract() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.extracting {
		return false
	}
	return t.turnCount >= t.TurnsBetween ||
		t.tokenDelta >= t.TokenThreshold ||
		t.toolCallCount >= t.ToolThreshold
}

// MarkExtracting marks an extraction as in progress.
func (t *SessionMemoryTracker) MarkExtracting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.extracting {
		return false
	}
	t.extracting = true
	return true
}

// MarkDone resets counters and clears extracting flag.
func (t *SessionMemoryTracker) MarkDone() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tokenDelta = 0
	t.toolCallCount = 0
	t.turnCount = 0
	t.extracting = false
}

// MarkFailed clears extracting flag without resetting counters.
func (t *SessionMemoryTracker) MarkFailed() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.extracting = false
}
