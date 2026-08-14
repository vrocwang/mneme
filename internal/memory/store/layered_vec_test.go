package store

import (
	"context"
	"testing"
)

// TestLayeredStore_SearchAtomsByVector verifies vec1-based vector search over
// L1 atoms returns the top-k by cosine similarity.
func TestLayeredStore_SearchAtomsByVector(t *testing.T) {
	db := openDB(t)
	ls, err := NewLayeredStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ids := []int64{}
	for _, v := range [][]float32{
		{1, 0, 0},
		{0.9, 0.1, 0},
		{0, 1, 0},
	} {
		id, err := ls.InsertAtom(ctx, Atom{
			Content:        "fact",
			Source:         "conversation",
			Vector:         v,
			EmbeddingModel: "test:3",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	res, err := ls.SearchAtomsByVector(ctx, []float32{1, 0, 0}, 3, "test:3")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res))
	}
	if res[0].Atom.ID != ids[0] {
		t.Errorf("expected closest atom id=%d, got %d", ids[0], res[0].Atom.ID)
	}
	if res[0].Similarity < 0.99 {
		t.Errorf("expected near-identical similarity, got %.6f", res[0].Similarity)
	}
	// Third atom (orthogonal) must rank last with ~0 similarity.
	if res[2].Atom.ID != ids[2] {
		t.Errorf("expected orthogonal atom id=%d last, got %d", ids[2], res[2].Atom.ID)
	}
}

// TestLayeredStore_SearchScenariosByVector verifies vec1-based vector search
// over L2 scenarios.
func TestLayeredStore_SearchScenariosByVector(t *testing.T) {
	db := openDB(t)
	ls, err := NewLayeredStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := ls.UpsertScenario(ctx, Scenario{Content: "s1", Vector: []float32{1, 0}, EmbeddingModel: "test:2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ls.UpsertScenario(ctx, Scenario{Content: "s2", Vector: []float32{0, 1}, EmbeddingModel: "test:2"}); err != nil {
		t.Fatal(err)
	}

	res, err := ls.SearchScenariosByVector(ctx, []float32{1, 0}, 2, "test:2")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	if res[0].Scenario.Content != "s1" {
		t.Errorf("expected s1 closest, got %q", res[0].Scenario.Content)
	}
}

// TestLayeredStore_SearchAtomsByVectorBruteForce exercises the fallback path.
func TestLayeredStore_SearchAtomsByVectorBruteForce(t *testing.T) {
	db := openDB(t)
	ls, err := NewLayeredStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, v := range [][]float32{{1, 0}, {0, 1}} {
		if _, err := ls.InsertAtom(ctx, Atom{Content: "f", Source: "c", Vector: v, EmbeddingModel: "test:2"}); err != nil {
			t.Fatal(err)
		}
	}

	res, err := ls.searchAtomsByVectorBruteForce(ctx, []float32{1, 0}, 2, "test:2")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 brute-force results, got %d", len(res))
	}
	if res[0].Similarity < res[1].Similarity {
		t.Errorf("brute-force results not sorted by similarity desc: %v", res)
	}
}
