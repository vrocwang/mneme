package subconscious

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// ── Memory Gap Evaluator ───────────────────────────────────────────
// Checks memory tree health and suggests review of under-represented topics.

// MemoryGapConfig tunes gap detection thresholds.
type MemoryGapConfig struct {
	// MinMessagesForGapCheck is the minimum messages before gap detection fires.
	MinMessagesForGapCheck int
	// ThinBranchThreshold is the minimum content length for a healthy branch.
	ThinBranchThreshold int
}

func DefaultMemoryGapConfig() MemoryGapConfig {
	return MemoryGapConfig{
		MinMessagesForGapCheck: 10,
		ThinBranchThreshold:    200,
	}
}

// MemoryGapEvaluator detects gaps in memory coverage and suggests review.
type MemoryGapEvaluator struct {
	log      *slog.Logger
	config   MemoryGapConfig
	pipeline MemoryPipeline

	// Track last nudge time to avoid spam.
	lastNudgeAt time.Time
	minInterval time.Duration
}

func NewMemoryGapEvaluator(log *slog.Logger) *MemoryGapEvaluator {
	return &MemoryGapEvaluator{
		log:         log,
		config:      DefaultMemoryGapConfig(),
		minInterval: 30 * time.Minute,
	}
}

// WithPipeline sets the memory pipeline for tree queries.
func (e *MemoryGapEvaluator) WithPipeline(p MemoryPipeline) *MemoryGapEvaluator {
	e.pipeline = p
	return e
}

func (e *MemoryGapEvaluator) Name() string { return "memory_gap" }

func (e *MemoryGapEvaluator) Evaluate(ctx context.Context, state *EngineState) ([]Action, error) {
	// Rate-limit: don't nudge more than once per interval.
	if time.Since(e.lastNudgeAt) < e.minInterval {
		return nil, nil
	}

	if e.pipeline == nil {
		return nil, nil
	}

	// Check for thin memory regions by probing diverse topics.
	probes := []string{
		"project architecture decisions",
		"recently completed tasks",
		"important deadlines or events",
		"user preferences and habits",
		"ongoing troubleshooting issues",
		"people and relationships",
	}

	gaps := make([]string, 0)
	for _, probe := range probes {
		result, err := e.pipeline.Search(probe, 5)
		if err != nil {
			continue
		}
		if result == nil || result.TotalCount < 2 {
			gaps = append(gaps, probe)
		}
	}

	if len(gaps) == 0 {
		return nil, nil
	}

	if len(gaps) < 3 {
		return nil, nil // not significant enough
	}

	e.lastNudgeAt = time.Now()

	// Build a reflection about memory coverage.
	gapList := ""
	for _, g := range gaps {
		gapList += fmt.Sprintf("- %s\n", g)
	}

	return []Action{
		{
			Type:    "suggestion",
			Title:   "Memory gaps detected",
			Message: fmt.Sprintf("Your memory has thin coverage in %d areas:\n%s\nConsider sharing more context about these topics to improve assistance.", len(gaps), gapList),
			Payload: map[string]interface{}{
				"gaps":  gaps,
				"count": len(gaps),
			},
		},
	}, nil
}

// ── Conversation Digest Evaluator ──────────────────────────────────
// Detects conversations that haven't been archived or summarized recently.

// ConversationDigestEvaluator checks for unprocessed conversations.
type ConversationDigestEvaluator struct {
	log         *slog.Logger
	pipeline    MemoryPipeline
	lastNudgeAt time.Time
	minInterval time.Duration
}

func NewConversationDigestEvaluator(log *slog.Logger) *ConversationDigestEvaluator {
	return &ConversationDigestEvaluator{
		log:         log,
		minInterval: 1 * time.Hour,
	}
}

func (e *ConversationDigestEvaluator) WithPipeline(p MemoryPipeline) *ConversationDigestEvaluator {
	e.pipeline = p
	return e
}

func (e *ConversationDigestEvaluator) Name() string { return "conversation_digest" }

func (e *ConversationDigestEvaluator) Evaluate(ctx context.Context, state *EngineState) ([]Action, error) {
	if time.Since(e.lastNudgeAt) < e.minInterval {
		return nil, nil
	}
	if e.pipeline == nil {
		return nil, nil
	}

	e.lastNudgeAt = time.Now()

	result, err := e.pipeline.Search("conversation summary", 3)
	if err != nil || (result != nil && result.TotalCount > 0) {
		return nil, nil
	}

	return []Action{
		{
			Type:    "reflection",
			Title:   "ConversationDigest",
			Message: "No recent conversation summaries found. The memory pipeline may need attention.",
			Payload: map[string]interface{}{"source": "conversation_digest"},
		},
	}, nil
}

// ── Idle Reminder Evaluator ────────────────────────────────────────
// Surfaces a reflection after prolonged inactivity.

// IdleReminderEvaluator suggests topics to revisit after inactivity.
type IdleReminderEvaluator struct {
	log      *slog.Logger
	pipeline MemoryPipeline

	lastActionAt  time.Time
	lastNudgeAt   time.Time
	idleThreshold time.Duration
	nudgeInterval time.Duration
}

func NewIdleReminderEvaluator(log *slog.Logger) *IdleReminderEvaluator {
	return &IdleReminderEvaluator{
		log:           log,
		idleThreshold: 2 * time.Hour,
		nudgeInterval: 1 * time.Hour,
	}
}

func (e *IdleReminderEvaluator) WithPipeline(p MemoryPipeline) *IdleReminderEvaluator {
	e.pipeline = p
	return e
}

func (e *IdleReminderEvaluator) Name() string { return "idle_reminder" }

func (e *IdleReminderEvaluator) NoteActivity() {
	e.lastActionAt = time.Now()
}

func (e *IdleReminderEvaluator) Evaluate(ctx context.Context, state *EngineState) ([]Action, error) {
	if e.lastActionAt.IsZero() {
		e.lastActionAt = time.Now()
		return nil, nil
	}

	idleDuration := time.Since(e.lastActionAt)
	if idleDuration < e.idleThreshold {
		return nil, nil
	}
	if time.Since(e.lastNudgeAt) < e.nudgeInterval {
		return nil, nil
	}

	e.lastNudgeAt = time.Now()

	idleHours := math.Round(idleDuration.Hours()*10) / 10

	return []Action{
		{
			Type:    "nudge",
			Title:   "Idle reminder",
			Message: fmt.Sprintf("It's been %.1f hours since your last activity. Want to pick up where you left off?", idleHours),
			Payload: map[string]interface{}{
				"idle_hours":     idleHours,
				"idle_threshold": e.idleThreshold.String(),
			},
		},
	}, nil
}
