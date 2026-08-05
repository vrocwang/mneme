package agent_experience

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStore_AddAndList(t *testing.T) {
	db := openDB(t)
	s := NewStore(db)

	s.Put(Record{AgentID: "researcher", Task: "Find docs about Go", Outcome: OutcomeSuccess, ToolsUsed: []string{"search", "read_file"}, ToolSequence: []string{"search", "read_file"}, Rounds: 3})
	s.Put(Record{AgentID: "coder", Task: "Fix bug in loop.go", Outcome: OutcomePartial, ToolsUsed: []string{"read_file", "edit_file"}, ToolSequence: []string{"read_file", "edit_file"}, Rounds: 5})
	s.Put(Record{AgentID: "researcher", Task: "Search for security issues", Outcome: OutcomeSuccess, ToolsUsed: []string{"search"}, ToolSequence: []string{"search"}, Rounds: 1})

	all, err := s.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 records, got %d", len(all))
	}

	// All three tasks should be present
	taskSet := make(map[string]bool)
	for _, r := range all {
		taskSet[r.Task] = true
	}
	for _, expected := range []string{"Find docs about Go", "Fix bug in loop.go", "Search for security issues"} {
		if !taskSet[expected] {
			t.Errorf("expected task %q not found in results", expected)
		}
	}
}

func TestStore_ListByAgent(t *testing.T) {
	db := openDB(t)
	s := NewStore(db)
	s.Put(Record{AgentID: "researcher", Task: "Task 1", Outcome: OutcomeSuccess, ToolsUsed: []string{"t"}, ToolSequence: []string{"t"}})
	s.Put(Record{AgentID: "coder", Task: "Task 2", Outcome: OutcomeSuccess, ToolsUsed: []string{"t"}, ToolSequence: []string{"t"}})
	s.Put(Record{AgentID: "researcher", Task: "Task 3", Outcome: OutcomeFailed, ToolsUsed: []string{"t"}, ToolSequence: []string{"t"}})

	results, err := s.ListByAgent("researcher", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 researcher records, got %d", len(results))
	}
}

func TestStore_RecentSuccesses(t *testing.T) {
	db := openDB(t)
	s := NewStore(db)
	s.Put(Record{AgentID: "coder", Task: "Success task", Outcome: OutcomeSuccess, ToolsUsed: []string{"t"}, ToolSequence: []string{"t"}})
	s.Put(Record{AgentID: "coder", Task: "Failed task", Outcome: OutcomeFailed, ToolsUsed: []string{"t"}, ToolSequence: []string{"t"}})
	s.Put(Record{AgentID: "coder", Task: "Another success", Outcome: OutcomeSuccess, ToolsUsed: []string{"t"}, ToolSequence: []string{"t"}})

	successes, err := s.RecentSuccesses("coder", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(successes) != 2 {
		t.Errorf("expected 2 successes, got %d", len(successes))
	}
	for _, r := range successes {
		if r.Outcome != OutcomeSuccess {
			t.Errorf("all should be success, got %s: %s", r.Outcome, r.Task)
		}
	}
}

func TestStore_FindSimilar(t *testing.T) {
	db := openDB(t)
	s := NewStore(db)
	s.Put(Record{AgentID: "researcher", Task: "Search for Go documentation on memory management", Outcome: OutcomeSuccess, ToolsUsed: []string{"search", "read_file"}, ToolSequence: []string{"search", "read_file"}, Confidence: 1.0, Rounds: 3})
	s.Put(Record{AgentID: "coder", Task: "Debug Python import error in main.py", Outcome: OutcomeSuccess, ToolsUsed: []string{"read_file"}, ToolSequence: []string{"read_file"}, Confidence: 1.0, Rounds: 5})
	s.Put(Record{AgentID: "researcher", Task: "Find Go documentation about goroutines and channels", Outcome: OutcomeSuccess, ToolsUsed: []string{"search"}, ToolSequence: []string{"search"}, Confidence: 1.0, Rounds: 2})

	hits, err := s.FindSimilar(Query{
		Text:    "Go memory documentation search",
		MaxHits: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Error("expected similar tasks for Go documentation query")
	}
	// First hit should be the "Go documentation" task
	if !contains(hits[0].MatchReasons, "query_overlap") && !contains(hits[0].MatchReasons, "overlap") {
		t.Logf("first hit: %s (score=%.2f, reasons=%v)", hits[0].Record.Task, hits[0].Score, hits[0].MatchReasons)
	}
}

func TestStore_Stats(t *testing.T) {
	db := openDB(t)
	s := NewStore(db)
	s.Put(Record{AgentID: "a", Task: "t1", Outcome: OutcomeSuccess, ToolsUsed: []string{"search"}, ToolSequence: []string{"search"}})
	s.Put(Record{AgentID: "a", Task: "t2", Outcome: OutcomeFailed, ToolsUsed: []string{"shell"}, ToolSequence: []string{"shell"}})
	s.Put(Record{AgentID: "a", Task: "t3", Outcome: OutcomePartial, ToolsUsed: []string{"search"}, ToolSequence: []string{"search"}})

	stats := s.Stats()
	total := stats["total"]
	// SQLite COUNT can return int64 or int depending on driver
	var totalVal int64
	switch v := total.(type) {
	case int64:
		totalVal = v
	case int:
		totalVal = int64(v)
	default:
		t.Fatalf("unexpected total type %T", total)
	}
	if totalVal != 3 {
		t.Errorf("expected 3 total, got %v", totalVal)
	}
}

func TestStore_Dismiss(t *testing.T) {
	db := openDB(t)
	s := NewStore(db)
	s.Put(Record{AgentID: "test", Task: "dismiss me", Outcome: OutcomeSuccess, ToolsUsed: []string{"t"}, ToolSequence: []string{"t"}})
	all, _ := s.List(10)
	if len(all) != 1 {
		t.Fatalf("expected 1 record before dismiss, got %d", len(all))
	}

	if err := s.Dismiss(all[0].ID); err != nil {
		t.Fatal(err)
	}
	all, _ = s.List(10)
	if len(all) != 0 {
		t.Errorf("expected 0 records after dismiss, got %d", len(all))
	}
}

func TestRedactText(t *testing.T) {
	redacted := redactText("token=abc123 password: hunter2 normal")
	if !containsStr(redacted, "[redacted]") {
		t.Errorf("expected redacted content, got: %s", redacted)
	}
	if containsStr(redacted, "abc123") {
		t.Error("should not contain raw token")
	}
}

func TestRenderForPrompt(t *testing.T) {
	hits := []Hit{
		{Record: Record{AgentID: "test", Task: "Do X", Lesson: "Use grep first", Outcome: OutcomeSuccess}, Score: 0.85},
	}
	out := RenderForPrompt(hits, 500)
	if out == "" || !containsStr(out, "Do X") {
		t.Errorf("unexpected render output: %s", out)
	}
}

func TestExtractCandidates_MultiToolSuccess(t *testing.T) {
	ctx := TurnContext{
		UserMessage: "Search the repo docs before opening the target file.",
		ToolCalls: []ToolCallRecord{
			{Name: "grep", Success: true, OutputSummary: "grep: ok"},
			{Name: "file_read", Success: true, OutputSummary: "file_read: ok"},
		},
		AgentID:        "orchestrator",
		Entrypoint:     "web_channel",
		TurnDurationMs: 1200,
	}
	candidates := ExtractCandidates(ctx)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Outcome != OutcomeSuccess {
		t.Errorf("expected success, got %s", candidates[0].Outcome)
	}
	if len(candidates[0].ToolSequence) != 2 {
		t.Errorf("expected 2-tool sequence, got %v", candidates[0].ToolSequence)
	}
}

func TestExtractCandidates_RepeatedFailures(t *testing.T) {
	ctx := TurnContext{
		UserMessage: "Try running the shell command.",
		ToolCalls: []ToolCallRecord{
			{Name: "shell", Success: false, OutputSummary: "shell: failed (permission_denied)"},
			{Name: "shell", Success: false, OutputSummary: "shell: failed (permission_denied)"},
			{Name: "grep", Success: true, OutputSummary: "grep: ok"},
		},
		AgentID: "test",
	}
	candidates := ExtractCandidates(ctx)
	if len(candidates) < 1 {
		t.Fatalf("expected at least 1 candidate, got %d", len(candidates))
	}
	// Should have a repeated-failure candidate for "shell"
	found := false
	for _, c := range candidates {
		if c.Outcome == OutcomeFailed && c.ErrorClass == "permission_denied" {
			found = true
		}
	}
	if !found {
		t.Error("expected a repeated-failure candidate for shell")
	}
}

func TestExtractCandidates_NoCalls(t *testing.T) {
	candidates := ExtractCandidates(TurnContext{UserMessage: "hello"})
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for empty tool calls, got %d", len(candidates))
	}
}

func contains(slice []string, substr string) bool {
	for _, s := range slice {
		if containsStr(s, substr) {
			return true
		}
	}
	return false
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
