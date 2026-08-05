package subconscious

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

// mockPipeline implements MemoryPipeline for testing.
type mockPipeline struct {
	results map[string]int
}

func (m *mockPipeline) Search(query string, limit int) (*MemorySearchResult, error) {
	count := m.results[query]
	return &MemorySearchResult{Query: query, TotalCount: count}, nil
}

func (m *mockPipeline) HasExternalContent(ctx context.Context, since time.Time) bool {
	return false
}

func TestEngine_Think(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	eng := New(log)

	pipe := &mockPipeline{
		results: map[string]int{
			"project architecture decisions": 0,
			"recently completed tasks":       0,
			"important deadlines or events":  1,
			"user preferences and habits":    0,
			"ongoing troubleshooting issues": 0,
			"people and relationships":       1,
		},
	}

	gapEval := NewMemoryGapEvaluator(log).WithPipeline(pipe)
	gapEval.minInterval = 0 // disable rate limiting for test
	eng.Register(gapEval)

	ctx := context.Background()
	actions := eng.Think(ctx)

	if len(actions) == 0 {
		t.Error("expected at least 1 action for 4 gaps")
	}

	found := false
	for _, a := range actions {
		if a.Type == "suggestion" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a suggestion action")
	}
}

func TestEngine_NoGapsNoActions(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	eng := New(log)

	pipe := &mockPipeline{
		results: map[string]int{
			"project architecture decisions": 5,
			"recently completed tasks":       5,
			"important deadlines or events":  5,
			"user preferences and habits":    5,
			"ongoing troubleshooting issues": 5,
			"people and relationships":       5,
		},
	}

	gapEval := NewMemoryGapEvaluator(log).WithPipeline(pipe)
	gapEval.minInterval = 0
	eng.Register(gapEval)

	ctx := context.Background()
	actions := eng.Think(ctx)

	if len(actions) != 0 {
		t.Errorf("expected 0 actions when no gaps, got %d", len(actions))
	}
}

func TestEngine_RateLimiting(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	eng := New(log)

	pipe := &mockPipeline{
		results: map[string]int{
			"project architecture decisions": 0,
			"recently completed tasks":       0,
			"important deadlines or events":  0,
			"user preferences and habits":    0,
			"ongoing troubleshooting issues": 0,
			"people and relationships":       0,
		},
	}

	gapEval := NewMemoryGapEvaluator(log).WithPipeline(pipe)
	// default minInterval is 30 min — should be rate-limited.
	eng.Register(gapEval)

	ctx := context.Background()
	actions1 := eng.Think(ctx)

	// Second call immediately after should be rate-limited.
	actions2 := eng.Think(ctx)

	if len(actions1) > 0 && len(actions2) > 0 {
		t.Error("second call should be rate-limited")
	}
}

func TestReflectionStore(t *testing.T) {
	rs := NewReflectionStore()

	rs.Add(Reflection{Kind: "Test", Body: "First reflection"})
	rs.Add(Reflection{Kind: "Test", Body: "Second reflection"})
	rs.Add(Reflection{Kind: "Test", Body: "Third reflection"})

	if rs.Count() != 3 {
		t.Errorf("expected 3 reflections, got %d", rs.Count())
	}

	list := rs.List(2)
	if len(list) != 2 {
		t.Errorf("expected 2 reflections, got %d", len(list))
	}

	// Most recent first.
	if list[0].Body != "Third reflection" {
		t.Errorf("expected 'Third reflection' first, got %q", list[0].Body)
	}
}

func TestEngine_Stats(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	eng := New(log)

	eng.Think(context.Background())
	eng.Think(context.Background())

	stats := eng.GetStats()
	if ticks, ok := stats["total_ticks"].(int64); !ok || ticks != 2 {
		t.Errorf("expected 2 ticks, got %v", stats["total_ticks"])
	}
}

func TestEngine_IdleReminder(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	eng := New(log)

	idleEval := NewIdleReminderEvaluator(log)
	idleEval.idleThreshold = 0 // immediately considered idle
	idleEval.nudgeInterval = 0 // always allow nudges
	// lastActionAt is zero, so NoteActivity will set it to now.
	idleEval.NoteActivity()

	// Force lastActionAt far into the past.
	idleEval.lastActionAt = time.Now().Add(-3 * time.Hour)

	eng.Register(idleEval)
	actions := eng.Think(context.Background())

	if len(actions) == 0 {
		t.Error("expected nudge after 3 hours idle")
	}
}

func TestConversationDigestEvaluator(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	eng := New(log)

	pipe := &mockPipeline{
		results: map[string]int{
			"conversation summary": 0,
		},
	}

	digestEval := NewConversationDigestEvaluator(log).WithPipeline(pipe)
	digestEval.minInterval = 0
	eng.Register(digestEval)

	actions := eng.Think(context.Background())

	if len(actions) == 0 {
		t.Error("expected reflection when no conversation summaries exist")
	}
	if len(actions) > 0 && actions[0].Type != "reflection" {
		t.Errorf("expected reflection type, got %s", actions[0].Type)
	}
}

func TestTaskStore(t *testing.T) {
	store := NewStore()

	task := ScheduledTask{
		ID:       "task-1",
		Name:     "Test Task",
		Schedule: "hourly",
		Enabled:  true,
	}
	store.AddTask(task)

	all := store.ListTasks()
	if len(all) != 1 {
		t.Fatalf("expected 1 task, got %d", len(all))
	}
	if all[0].ID != "task-1" {
		t.Errorf("expected task-1, got %s", all[0].ID)
	}
}

func TestMemorySearchResult(t *testing.T) {
	result := &MemorySearchResult{Query: "test", TotalCount: 5}
	if result.TotalCount != 5 {
		t.Errorf("expected 5, got %d", result.TotalCount)
	}
	fmt.Println(result)
}
