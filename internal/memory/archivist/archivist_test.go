package archivist

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestArchivist_HeuristicSummary_NoProvider(t *testing.T) {
	a := New(slog.New(slog.NewTextHandler(os.Stderr, nil)), nil, "")

	content := "The user is building a Go-based desktop AI assistant called Mneme Go. " +
		"It uses Wails v2 for the desktop shell and SQLite for persistence. " +
		"The agent loop supports tool calling with up to 10 rounds per turn. " +
		"Security includes command classification, permission gates, and prompt injection detection."

	result, err := a.SummarizeMemory(context.Background(), content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if result.ShouldPrune {
		t.Error("substantive content should not be pruned")
	}
}

func TestArchivist_HeuristicSummary_TrivialContent(t *testing.T) {
	a := New(slog.New(slog.NewTextHandler(os.Stderr, nil)), nil, "")

	result, err := a.SummarizeMemory(context.Background(), "OK thanks!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ShouldPrune {
		t.Error("trivial content should be marked for pruning")
	}
}

func TestArchivist_Deduplicate_Similar(t *testing.T) {
	a := New(slog.New(slog.NewTextHandler(os.Stderr, nil)), nil, "")

	a1 := "The project uses Go with Wails v2 for desktop applications."
	a2 := "The project uses Go with Wails v2 for building desktop apps."

	score, _, err := a.Deduplicate(context.Background(), a1, a2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score < 0.3 {
		t.Errorf("similar texts should have score >= 0.3, got %.2f", score)
	}
}

func TestArchivist_Deduplicate_Different(t *testing.T) {
	a := New(slog.New(slog.NewTextHandler(os.Stderr, nil)), nil, "")

	a1 := "The project uses Go with Wails v2 for desktop applications."
	a2 := "Weather forecast predicts rain tomorrow afternoon in Tokyo."

	score, _, err := a.Deduplicate(context.Background(), a1, a2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score > 0.3 {
		t.Errorf("different texts should have low score, got %.2f", score)
	}
}

func TestArchivist_ExtractFacts(t *testing.T) {
	a := New(slog.New(slog.NewTextHandler(os.Stderr, nil)), nil, "")

	content := "The Go rewrite targets a 15-week timeline. Phase 1 covers project scaffolding. " +
		"Phase 2 implements the security foundation. Phase 3 builds the agent engine core. " +
		"Wails v2 provides desktop bindings between Go and React."

	facts, err := a.ExtractFacts(context.Background(), content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(facts) == 0 {
		t.Error("expected at least one fact")
	}
	if len(facts) > 5 {
		t.Logf("got %d facts (capped at 5 heuristic): %v", len(facts), facts)
	}
}

func TestHeuristicSummary_LongContent(t *testing.T) {
	// Build content > 500 chars with multiple sentences.
	content := ""
	for i := 0; i < 20; i++ {
		content += "This is sentence number one with enough length. "
	}
	result := heuristicSummary(content)
	if len(result.Summary) >= len(content) {
		t.Error("long content should be summarized to shorter form")
	}
}

func TestSimpleSimilarity(t *testing.T) {
	score := simpleSimilarity("hello world foo bar", "hello world baz qux")
	if score <= 0 {
		t.Error("expected positive similarity")
	}
	if score >= 1.0 {
		t.Error("non-identical texts should have score < 1.0")
	}

	score = simpleSimilarity("completely", "different")
	if score != 0 {
		t.Errorf("completely different texts should have score 0, got %.2f", score)
	}
}

func TestExtractEntitiesHeuristic(t *testing.T) {
	entities := extractEntitiesHeuristic("Alice and Bob work at AcmeCorp on ProjectX. Charlie manages the Backend team.")
	if len(entities) == 0 {
		t.Error("expected entities from capitalized words")
	}
}
