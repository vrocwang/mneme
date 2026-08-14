package middleware

import (
	"fmt"
	"hash/fnv"
	"sync"
)

// CircuitBreakerMiddleware detects and interrupts pathological agent
// loops. It monitors three signals:
//
//   - Repeat failures: the same tool+args combination failing 3+ times.
//   - Consecutive failures: 6+ tool failures in a row (any tool).
//   - Narration loops: 4+ identical model outputs in sequence.
//
// When any signal triggers, the breaker trips and stays tripped until
// explicitly reset. Callers should check IsTripped() before each tool
// invocation and before sending a model response to the user.
//
// All methods are safe for concurrent use.
type CircuitBreakerMiddleware struct {
	mu                sync.Mutex
	repeatFailures    map[string]int // tool+args hash -> count
	consecutiveFails  int
	lastOutputHash    string
	repeatOutputCount int
	tripped           bool
	tripReason        string

	// MaxRepeatFailures is the number of times the same tool+args
	// combination can fail before the breaker trips. Default: 3.
	MaxRepeatFailures int

	// MaxConsecutiveFails is the number of tool failures that can
	// occur in a row (any tool) before the breaker trips. Default: 6.
	MaxConsecutiveFails int

	// MaxRepeatOutputs is the number of identical model outputs that
	// can appear in sequence before the breaker trips. Default: 4.
	MaxRepeatOutputs int
}

// NewCircuitBreaker returns a ready-to-use CircuitBreakerMiddleware
// with sensible defaults (3 repeat failures, 6 consecutive failures,
// 4 repeat outputs).
func NewCircuitBreaker() *CircuitBreakerMiddleware {
	return &CircuitBreakerMiddleware{
		repeatFailures:      make(map[string]int),
		MaxRepeatFailures:   3,
		MaxConsecutiveFails: 6,
		MaxRepeatOutputs:    4,
	}
}

// NewCircuitBreakerWithConfig returns a breaker with explicit thresholds.
// Zero values fall back to the defaults.
func NewCircuitBreakerWithConfig(maxRepeatFailures, maxConsecutiveFails, maxRepeatOutputs int) *CircuitBreakerMiddleware {
	cb := NewCircuitBreaker()
	if maxRepeatFailures > 0 {
		cb.MaxRepeatFailures = maxRepeatFailures
	}
	if maxConsecutiveFails > 0 {
		cb.MaxConsecutiveFails = maxConsecutiveFails
	}
	if maxRepeatOutputs > 0 {
		cb.MaxRepeatOutputs = maxRepeatOutputs
	}
	return cb
}

// RecordFailure records a tool execution failure. It increments the
// repeat-failure counter for the specific tool+args combination (keyed
// by SHA-256 hash) and increments the consecutive-failure counter.
//
// Returns true if the breaker trips as a result of this failure.
func (c *CircuitBreakerMiddleware) RecordFailure(toolName, args string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tripped {
		return true
	}

	key := hashToolCall(toolName, args)

	c.repeatFailures[key]++
	c.consecutiveFails++

	// Check repeat-failure threshold.
	if c.repeatFailures[key] >= c.MaxRepeatFailures {
		c.tripped = true
		c.tripReason = fmt.Sprintf(
			"repeat failure: tool=%q failed %d times with same args",
			toolName, c.repeatFailures[key],
		)
		return true
	}

	// Check consecutive-failure threshold.
	if c.consecutiveFails >= c.MaxConsecutiveFails {
		c.tripped = true
		c.tripReason = fmt.Sprintf(
			"consecutive failures: %d tool failures in a row",
			c.consecutiveFails,
		)
		return true
	}

	return false
}

// RecordOutput checks the model output for narration loops. If the
// same output (by SHA-256 hash) appears MaxRepeatOutputs times in a
// row, the breaker trips.
//
// Returns true if the breaker trips as a result of this output.
func (c *CircuitBreakerMiddleware) RecordOutput(output string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tripped {
		return true
	}

	hash := hashString(output)

	if hash == c.lastOutputHash {
		c.repeatOutputCount++
	} else {
		c.lastOutputHash = hash
		c.repeatOutputCount = 1
	}

	if c.repeatOutputCount >= c.MaxRepeatOutputs {
		c.tripped = true
		c.tripReason = fmt.Sprintf(
			"narration loop: %d identical outputs in a row",
			c.repeatOutputCount,
		)
		return true
	}

	return false
}

// RecordSuccess resets the consecutive-failure counter. Call this after
// any successful tool execution to prevent false-positive trips from
// intermittent failures.
func (c *CircuitBreakerMiddleware) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFails = 0
}

// IsTripped returns true when the breaker has been triggered.
func (c *CircuitBreakerMiddleware) IsTripped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tripped
}

// Reason returns a human-readable description of why the breaker
// tripped. Returns an empty string when the breaker is not tripped.
func (c *CircuitBreakerMiddleware) Reason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tripReason
}

// Reset clears all counters and re-arms the breaker. Use this when the
// user acknowledges the loop and wants to continue, or when the
// conversation context has changed sufficiently that the old failure
// state is no longer relevant.
func (c *CircuitBreakerMiddleware) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.repeatFailures = make(map[string]int)
	c.consecutiveFails = 0
	c.lastOutputHash = ""
	c.repeatOutputCount = 0
	c.tripped = false
	c.tripReason = ""
}

// ── helpers ────────────────────────────────────────────────────────────

// hashToolCall produces a stable key for a tool+args pair using SHA-256.
func hashToolCall(toolName, args string) string {
	return hashString(toolName + "\x00" + args)
}

// hashString returns a hex string of the FNV-1a 64-bit hash of s.
func hashString(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum64())
}
