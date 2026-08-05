package memory

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// SessionMemoryConfig holds thresholds for automatic MEMORY.md extraction.
type SessionMemoryConfig struct {
	MinTokenGrowth  int `json:"min_token_growth"`  // trigger after N tokens since last extract (default 4000)
	MinToolCalls    int `json:"min_tool_calls"`    // trigger after N tool calls since last extract (default 8)
	MinTurnsBetween int `json:"min_turns_between"` // minimum turns between extractions (default 4)
}

// DefaultSessionMemoryConfig returns sensible defaults.
func DefaultSessionMemoryConfig() SessionMemoryConfig {
	return SessionMemoryConfig{MinTokenGrowth: 4000, MinToolCalls: 8, MinTurnsBetween: 4}
}

// SessionMemoryState tracks extraction thresholds for a session.
type SessionMemoryState struct {
	mu                     sync.Mutex
	totalTokens            int
	tokensAtLastExtract    int
	turnAtLastExtract      int
	totalToolCalls         int
	toolCallsAtLastExtract int
	currentTurn            int
	inProgress             bool
}

// SessionMemory orchestrates automatic MEMORY.md extraction.
type SessionMemory struct {
	config    SessionMemoryConfig
	state     *SessionMemoryState
	workspace string
	log       *slog.Logger
	extractFn func(summary string) error // called when extraction triggers
}

// NewSessionMemory creates a session memory tracker.
func NewSessionMemory(workspace string, config SessionMemoryConfig, log *slog.Logger) *SessionMemory {
	if log == nil {
		log = slog.Default()
	}
	return &SessionMemory{
		config:    config,
		state:     &SessionMemoryState{},
		workspace: workspace,
		log:       log.With("component", "session-memory"),
	}
}

// SetExtractFn sets the callback invoked when extraction triggers.
func (sm *SessionMemory) SetExtractFn(fn func(summary string) error) {
	sm.extractFn = fn
}

// TickTurn increments the turn counter and checks thresholds.
func (sm *SessionMemory) TickTurn() bool {
	sm.state.mu.Lock()
	defer sm.state.mu.Unlock()

	sm.state.currentTurn++

	if sm.state.inProgress {
		return false
	}

	if sm.state.currentTurn-sm.state.turnAtLastExtract < sm.config.MinTurnsBetween {
		return false
	}

	tokenGrowth := sm.state.totalTokens - sm.state.tokensAtLastExtract
	toolCallGrowth := sm.state.totalToolCalls - sm.state.toolCallsAtLastExtract

	if tokenGrowth >= sm.config.MinTokenGrowth || toolCallGrowth >= sm.config.MinToolCalls {
		sm.state.inProgress = true
		return true
	}
	return false
}

// RecordUsage updates token counts.
func (sm *SessionMemory) RecordUsage(promptTokens, completionTokens int) {
	sm.state.mu.Lock()
	defer sm.state.mu.Unlock()
	sm.state.totalTokens += promptTokens + completionTokens
}

// RecordToolCalls records tool call counts.
func (sm *SessionMemory) RecordToolCalls(count int) {
	sm.state.mu.Lock()
	defer sm.state.mu.Unlock()
	sm.state.totalToolCalls += count
}

// MarkComplete finalizes an extraction attempt.
func (sm *SessionMemory) MarkComplete(success bool) {
	sm.state.mu.Lock()
	defer sm.state.mu.Unlock()
	sm.state.inProgress = false
	if success {
		sm.state.tokensAtLastExtract = sm.state.totalTokens
		sm.state.toolCallsAtLastExtract = sm.state.totalToolCalls
		sm.state.turnAtLastExtract = sm.state.currentTurn
	}
}

// MemoryFilePath returns the path to the MEMORY.md file.
func (sm *SessionMemory) MemoryFilePath() string {
	return filepath.Join(sm.workspace, "MEMORY.md")
}

// ReadMemoryFile returns current MEMORY.md content.
func (sm *SessionMemory) ReadMemoryFile() (string, error) {
	data, err := os.ReadFile(sm.MemoryFilePath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read MEMORY.md: %w", err)
	}
	return string(data), nil
}

// WriteMemoryFile writes content to MEMORY.md.
func (sm *SessionMemory) WriteMemoryFile(content string) error {
	dir := filepath.Dir(sm.MemoryFilePath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	return os.WriteFile(sm.MemoryFilePath(), []byte(content), 0644)
}

// Snapshot returns a summary of the current state for diagnostics.
func (sm *SessionMemory) Snapshot() map[string]interface{} {
	sm.state.mu.Lock()
	defer sm.state.mu.Unlock()
	return map[string]interface{}{
		"total_tokens":           sm.state.totalTokens,
		"tokens_at_last_extract": sm.state.tokensAtLastExtract,
		"total_tool_calls":       sm.state.totalToolCalls,
		"current_turn":           sm.state.currentTurn,
		"in_progress":            sm.state.inProgress,
	}
}

// ExtractionPrompt is the system prompt for the background archivist agent.
const ExtractionPrompt = `You are a memory archivist. Your job is to extract durable facts, preferences,
decisions, and knowledge from the recent conversation and update MEMORY.md.

Instructions:
1. Read the current MEMORY.md to understand what is already stored.
2. Identify NEW facts about the user: preferences, goals, decisions, knowledge about systems, unresolved tasks.
3. Do NOT duplicate existing entries. Merge with existing content when facts evolve.
4. Write concise, searchable entries. Use bullet points under ## sections.
5. Include dates for time-sensitive facts.
6. Use the update_memory_md tool to write the updated file.

Focus on facts that will be useful in FUTURE conversations. Skip transient details.`
