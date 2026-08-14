package store

import (
	"context"
	"sort"
	"testing"
)

// TestSearchByVector_MatchesBruteForce verifies that the vec1-based
// SearchByVector returns the same top-k ordering (and near-identical
// similarities) as an independent Go-side brute-force cosine scan over the
// same chunk set. This is the stage-4 consistency acceptance check.
func TestSearchByVector_MatchesBruteForce(t *testing.T) {
	db := openDB(t)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	// Deterministic pseudo-embeddings: 10 four-dimensional vectors with a
	// shared axis so cosine similarities span a meaningful range.
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0.9, 0.4, 0, 0},
		{0.7, 0.7, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
		{0.5, 0.5, 0.5, 0.5},
		{0.1, 0.9, 0.1, 0},
		{0.3, 0.3, 0.3, 0.9},
		{1, 1, 0, 0},
	}
	const modelSig = "test:4"

	for i, v := range vectors {
		if _, err := s.Insert(MemoryChunk{
			Source:         "conversation",
			Content:        "chunk " + string(rune('a'+i)),
			Vector:         v,
			EmbeddingModel: modelSig,
		}); err != nil {
			t.Fatal(err)
		}
	}

	query := []float32{1, 0.2, 0, 0}
	const k = 5

	got, err := s.SearchByVector(query, k, modelSig)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != k {
		t.Fatalf("expected %d results, got %d", k, len(got))
	}

	// Independent brute-force reference over the same inserted chunks.
	chunks, err := s.ListByModel(context.Background(), modelSig)
	if err != nil {
		t.Fatal(err)
	}
	type ref struct {
		id  int64
		sim float64
	}
	var refs []ref
	for _, c := range chunks {
		if len(c.Vector) == 0 {
			continue
		}
		refs = append(refs, ref{id: c.ID, sim: CosineSimilarity(query, c.Vector)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].sim > refs[j].sim })
	if len(refs) > k {
		refs = refs[:k]
	}

	if len(got) != len(refs) {
		t.Fatalf("vec1 returned %d, brute force %d", len(got), len(refs))
	}

	// The top-k SET and per-item similarity values must match exactly. Rank
	// ordering is compared separately below (ties are ambiguous, so we assert
	// the same set + same scores rather than a strict order on tied items).
	refByID := make(map[int64]float64, len(refs))
	for _, r := range refs {
		refByID[r.id] = r.sim
	}
	for _, g := range got {
		want, ok := refByID[g.Chunk.ID]
		if !ok {
			t.Errorf("vec1 returned id=%d not in brute-force top-%d", g.Chunk.ID, k)
			continue
		}
		if delta := g.Similarity - want; delta > 1e-4 || delta < -1e-4 {
			t.Errorf("id=%d similarity %.6f vs %.6f", g.Chunk.ID, g.Similarity, want)
		}
	}

	// And the ranking must be non-increasing similarity (distance ascending).
	for i := 1; i < len(got); i++ {
		if got[i].Similarity > got[i-1].Similarity+1e-6 {
			t.Errorf("results not sorted by similarity desc: got[%d]=%.6f > got[%d]=%.6f",
				i, got[i].Similarity, i-1, got[i-1].Similarity)
		}
	}
}

// TestSearchByVector_EmptyQuery ensures a nil/empty query short-circuits.
func TestSearchByVector_EmptyQuery(t *testing.T) {
	db := openDB(t)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.SearchByVector(nil, 10, "test:4")
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("expected nil results for empty query, got %v", res)
	}
}

// TestSearchByVector_BruteForceFallback exercises the fallback path directly.
func TestSearchByVector_BruteForceFallback(t *testing.T) {
	db := openDB(t)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Insert(MemoryChunk{Source: "conversation", Content: "a", Vector: []float32{1, 0}, EmbeddingModel: "test:2"})
	_, _ = s.Insert(MemoryChunk{Source: "conversation", Content: "b", Vector: []float32{0, 1}, EmbeddingModel: "test:2"})

	res, err := s.searchByVectorBruteForce([]float32{1, 0}, 2, "test:2")
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
