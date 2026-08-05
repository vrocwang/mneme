package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/tokenjuice"
)

// TurnSnapshot is a complete record of a turn for post-turn hooks.
type TurnSnapshot struct {
	ThreadID     string
	AgentName    string
	AgentID      string
	Model        string
	UserMessage  string
	Response     string
	ToolCalls    []ToolCallResult
	TotalRounds  int
	Duration     time.Duration
	InputTokens  int
	OutputTokens int
	Error        error
	StartedAt    time.Time
	CompletedAt  time.Time
}

// PostTurnHook is an async hook that fires after a turn completes.
// It receives a full snapshot and runs in its own goroutine.
type PostTurnHook interface {
	Name() string
	OnTurnComplete(ctx context.Context, snapshot *TurnSnapshot)
}

// PostTurnHookFunc wraps a function as a PostTurnHook.
type PostTurnHookFunc struct {
	name string
	fn   func(ctx context.Context, snapshot *TurnSnapshot)
}

func (h *PostTurnHookFunc) Name() string { return h.name }

func (h *PostTurnHookFunc) OnTurnComplete(ctx context.Context, snapshot *TurnSnapshot) {
	h.fn(ctx, snapshot)
}

// NewPostTurnHook creates a named post-turn hook from a function.
func NewPostTurnHook(name string, fn func(ctx context.Context, snapshot *TurnSnapshot)) PostTurnHook {
	return &PostTurnHookFunc{name: name, fn: fn}
}

// PostTurnHookRegistry manages async post-turn hooks.
type PostTurnHookRegistry struct {
	mu    sync.RWMutex
	hooks []PostTurnHook
	log   *slog.Logger
}

// NewPostTurnHookRegistry creates a registry.
func NewPostTurnHookRegistry() *PostTurnHookRegistry {
	return &PostTurnHookRegistry{}
}

// WithLogger sets a logger for panic reporting.
func (r *PostTurnHookRegistry) WithLogger(l *slog.Logger) *PostTurnHookRegistry {
	r.log = l
	return r
}

// Register adds a hook.
func (r *PostTurnHookRegistry) Register(hook PostTurnHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, hook)
}

// Fire spawns all registered hooks asynchronously.
func (r *PostTurnHookRegistry) Fire(ctx context.Context, snapshot *TurnSnapshot) {
	r.mu.RLock()
	hooks := make([]PostTurnHook, len(r.hooks))
	copy(hooks, r.hooks)
	r.mu.RUnlock()

	for _, h := range hooks {
		hook := h
		go func() {
			defer func() {
				if p := recover(); p != nil {
					if r.log != nil {
						r.log.Error("post-turn hook panicked", "panic", fmt.Sprint(p))
					}
				}
			}()
			hook.OnTurnComplete(ctx, snapshot)
		}()
	}
}

// ── Built-in hooks ────────────────────────────────────────────────────────

// CostTrackingHook records token costs after each turn.
type CostTrackingHook struct {
	tracker CostTracker
}

// CostTracker is the interface for cost tracking.
type CostTracker interface {
	Record(model string, inputTokens, outputTokens int)
}

func NewCostTrackingHook(tracker CostTracker) *CostTrackingHook {
	return &CostTrackingHook{tracker: tracker}
}

func (h *CostTrackingHook) Name() string { return "cost-tracking" }

func (h *CostTrackingHook) OnTurnComplete(ctx context.Context, s *TurnSnapshot) {
	h.tracker.Record(s.Model, s.InputTokens, s.OutputTokens)
}

// LearningReflectHook triggers learning reflection after successful turns.
type LearningReflectHook struct {
	fn func(ctx context.Context, threadID, userMessage, response string)
}

func NewLearningReflectHook(reflectFn func(ctx context.Context, threadID, userMessage, response string)) *LearningReflectHook {
	return &LearningReflectHook{fn: reflectFn}
}

func (h *LearningReflectHook) Name() string { return "learning-reflect" }

func (h *LearningReflectHook) OnTurnComplete(ctx context.Context, s *TurnSnapshot) {
	if s.Error != nil || h.fn == nil {
		return
	}
	h.fn(ctx, s.ThreadID, s.UserMessage, s.Response)
}

// MemoryArchiveHook triggers conversation archiving after turns.
type MemoryArchiveHook struct {
	pipeline MemoryArchiver
}

// MemoryArchiver archives conversations to the memory pipeline.
type MemoryArchiver interface {
	ArchiveConversation(threadID string)
}

func NewMemoryArchiveHook(pipeline MemoryArchiver) *MemoryArchiveHook {
	return &MemoryArchiveHook{pipeline: pipeline}
}

func (h *MemoryArchiveHook) Name() string { return "memory-archive" }

func (h *MemoryArchiveHook) OnTurnComplete(ctx context.Context, s *TurnSnapshot) {
	if s.ThreadID == "" {
		return
	}
	h.pipeline.ArchiveConversation(s.ThreadID)
}

// ── Turn context helpers ───────────────────────────────────────────────────

// BuildTurnSnapshot creates a snapshot from the raw turn components.
func BuildTurnSnapshot(
	threadID, agentName, agentID, model, userMessage, response string,
	toolCalls []ToolCallResult,
	totalRounds int,
	duration time.Duration,
	inputTokens, outputTokens int,
	err error,
	startedAt time.Time,
) *TurnSnapshot {
	return &TurnSnapshot{
		ThreadID:     threadID,
		AgentName:    agentName,
		AgentID:      agentID,
		Model:        model,
		UserMessage:  userMessage,
		Response:     response,
		ToolCalls:    toolCalls,
		TotalRounds:  totalRounds,
		Duration:     duration,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Error:        err,
		StartedAt:    startedAt,
		CompletedAt:  time.Now(),
	}
}

// EstimateTokens returns a proper token count for text using the
// CJK-aware heuristic (substantially more accurate than the old len/4).
func EstimateTokens(text string) int {
	return tokenjuice.CountTokens(text)
}

// EstimateMessagesTokens sums token counts for a message list.
func EstimateMessagesTokens(messages []inference.Message) int {
	total := 0
	for _, m := range messages {
		total += tokenjuice.CountTokens(m.Content)
	}
	return total
}
