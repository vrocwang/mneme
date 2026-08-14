package graph

import (
	"database/sql"
	"testing"

	_ "github.com/simon/mneme/internal/sqlite"
)

func TestGraph_RecordCoOccurrence(t *testing.T) {
	g, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	g.RecordCoOccurrence([]string{"Alice", "Bob"})
	g.RecordCoOccurrence([]string{"Alice", "Bob"})
	g.RecordCoOccurrence([]string{"Alice", "Charlie"})

	related := g.GetRelated("Alice", 10, 1)
	if len(related) != 2 {
		t.Fatalf("expected 2 related entities, got %d", len(related))
	}

	// Bob should have higher weight (2 co-occurrences vs 1)
	if related[0].Target != "Bob" && related[1].Target != "Bob" {
		t.Error("expected Bob in related entities")
	}
	if related[0].Weight <= related[1].Weight {
		t.Errorf("expected Bob (2 co-occurrences) to rank higher than Charlie (1), got weights: %.2f vs %.2f",
			related[0].Weight, related[1].Weight)
	}
}

func TestGraph_DeduplicationAndNormalization(t *testing.T) {
	g, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Same entity multiple times should be deduped
	g.RecordCoOccurrence([]string{"Alice", "alice", "ALICE", "Bob"})

	related := g.GetRelated("Alice", 10, 1)
	if len(related) != 1 {
		t.Fatalf("expected 1 related entity, got %d", len(related))
	}
	if related[0].Target != "Bob" {
		t.Errorf("expected Bob, got %s", related[0].Target)
	}
}

func TestGraph_GetRelated_EmptyGraph(t *testing.T) {
	g, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	related := g.GetRelated("Nobody", 10, 1)
	if len(related) != 0 {
		t.Errorf("expected 0 results, got %d", len(related))
	}
}

func TestGraph_MultiHopTraversal(t *testing.T) {
	g, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Build: Alice-Bob (5x), Bob-Charlie (3x), Alice-Dave (1x)
	for i := 0; i < 5; i++ {
		g.RecordCoOccurrence([]string{"Alice", "Bob"})
	}
	for i := 0; i < 3; i++ {
		g.RecordCoOccurrence([]string{"Bob", "Charlie"})
	}
	g.RecordCoOccurrence([]string{"Alice", "Dave"})

	// Single hop: should find Bob and Dave
	direct := g.GetRelated("Alice", 10, 1)
	if len(direct) != 2 {
		t.Errorf("expected 2 direct relations, got %d", len(direct))
	}

	// Multi-hop: should also find Charlie (via Bob) at depth 2
	multiHop := g.GetRelated("Alice", 10, 2)
	if len(multiHop) < 3 {
		t.Errorf("expected at least 3 results with multi-hop (Bob, Dave, Charlie), got %d", len(multiHop))
	}
	foundCharlie := false
	for _, e := range multiHop {
		if e.Target == "Charlie" {
			foundCharlie = true
		}
	}
	if !foundCharlie {
		t.Error("expected Charlie via multi-hop traversal at depth 2")
	}
}

func TestGraph_Persistence(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	g, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	g.RecordCoOccurrencePersisted([]string{"Alice", "Bob", "Charlie"})

	// Load a new graph from the same DB and verify edges are restored
	g2, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	related := g2.GetRelated("Alice", 10, 1)
	if len(related) != 2 {
		t.Fatalf("expected 2 related after reload, got %d", len(related))
	}
}

func TestGraph_Stats(t *testing.T) {
	g, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	g.RecordCoOccurrence([]string{"A", "B"})
	g.RecordCoOccurrence([]string{"B", "C"})
	g.RecordCoOccurrence([]string{"A", "C"})

	stats := g.Stats()
	if stats["edges"].(int) != 3 {
		t.Errorf("expected 3 edges, got %d", stats["edges"])
	}
	if stats["nodes"].(int) != 3 {
		t.Errorf("expected 3 nodes, got %d", stats["nodes"])
	}
}

func TestGraph_EdgeKeyDeterministic(t *testing.T) {
	k1 := edgeKey("Alice", "Bob")
	k2 := edgeKey("Bob", "Alice")
	if k1 != k2 {
		t.Errorf("edge keys should be symmetric: %q vs %q", k1, k2)
	}
	if k1 != "alice::bob" {
		t.Errorf("expected alice::bob, got %s", k1)
	}
}

func TestFormatRelated(t *testing.T) {
	edges := []Edge{
		{Source: "Alice", Target: "Bob", Weight: 2.0, Count: 3},
	}
	result := FormatRelated("Alice", edges)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestFormatRelated_Empty(t *testing.T) {
	result := FormatRelated("Nobody", nil)
	if result == "" {
		t.Error("expected non-empty result for empty edges")
	}
}
