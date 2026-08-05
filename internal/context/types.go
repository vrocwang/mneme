package context

import (
	"sync"
	"time"

	"github.com/simon/mneme/internal/tokenjuice"
)

// Message is a conversation message used in context assembly.
type Message struct {
	Role      string // "system", "user", "assistant", "tool"
	Content   string
	Thinking  string // extended thinking content (Claude/DeepSeek thinking mode)
	Signature string // thinking signature for verification
	Tokens    int    // estimated token count
	Timestamp time.Time
	ToolCall  *ToolCallRef // non-nil for assistant messages that contain a native tool call
	ToolID    string       // tool call ID for tool result messages
}

// ToolCallRef is a lightweight reference to an LLM tool call.
type ToolCallRef struct {
	ID   string
	Name string
}

// PreferencePair is a learned user preference (key-value with confidence).
type PreferencePair struct {
	Key        string
	Value      string
	Confidence float64
}

// PromptContext carries all the runtime values available during prompt rendering.
type PromptContext struct {
	AgentName    string
	Date         time.Time
	Tools        []ToolInfo
	Workspace    string           // workspace root path
	Skills       []string         // injected skill names
	Preferences  []PreferencePair // learned user preferences
	RecentMemory string           // recent memory search results
}

// ToolInfo is the minimal tool description needed for prompt injection.
type ToolInfo struct {
	Name        string
	Description string
}

// CompactResult is the result of a compaction operation (microcompact or LLM summarization).
type CompactResult struct {
	Action         string    // "none", "microcompact", "autocompact"
	MessagesAfter  []Message // messages after compaction
	SummaryApplied string    // summary text (for autocompact)
}

// HistoryStats summarizes the conversation history.
type HistoryStats struct {
	MessageCount  int
	TotalTokens   int
	MaxTokens     int
	ToolCallCount int
	UserTurnCount int
}

// ComputeHistoryStats computes statistics about the message history.
func ComputeHistoryStats(msgs []Message) HistoryStats {
	stats := HistoryStats{MessageCount: len(msgs)}
	for _, m := range msgs {
		tokens := m.Tokens
		if tokens == 0 {
			tokens = tokenjuice.CountTokens(m.Content)
		}
		stats.TotalTokens += tokens
		if tokens > stats.MaxTokens {
			stats.MaxTokens = tokens
		}
		if m.Role == "tool" || m.ToolCall != nil {
			stats.ToolCallCount++
		}
		if m.Role == "user" {
			stats.UserTurnCount++
		}
	}
	return stats
}

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

// GuardResult is the result of a context guard check.
type GuardResult struct {
	OK              bool   `json:"ok"`
	Action          string `json:"action"` // "proceed", "compact", "blocked"
	Reason          string `json:"reason,omitempty"`
	EstimatedTokens int    `json:"estimated_tokens"`
	MaxTokens       int    `json:"max_tokens"`
}
