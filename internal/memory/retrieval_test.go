package memory

import (
	"context"
	"database/sql"
	"testing"

	"github.com/simon/mneme/internal/memory/store"
	"github.com/simon/mneme/internal/memory/tree"
)

func TestRetrievalWeights_Normalize(t *testing.T) {
	w := DefaultWeights()
	sum := w.FTS5 + w.Vector + w.Keyword + w.Tree + w.Graph + w.Episodic
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("weights should sum to ~1.0, got %.4f", sum)
	}
}

func TestMultiStrategyRetriever_Search_FTS5Only(t *testing.T) {
	db := newTestDB(t)
	memStore, _ := store.NewStore(db)
	memStore.Insert(store.MemoryChunk{Source: "test", Content: "hello world from Go"})
	memStore.Insert(store.MemoryChunk{Source: "test", Content: "Rust programming language"})
	memTree := tree.NewTree(5)
	memTree.Add("root", "node1", "Go is great for services")

	w := RetrievalWeights{FTS5: 1.0, Vector: 0, Keyword: 0, Tree: 0, Graph: 0, Episodic: 0}
	retriever := NewMultiStrategyRetriever(memStore, memTree, nil, w, nil)

	results, err := retriever.Search(context.Background(), "Go", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least 1 result for 'Go'")
	}
}

func TestMultiStrategyRetriever_Search_Tree(t *testing.T) {
	db := newTestDB(t)
	memStore, _ := store.NewStore(db)
	memTree := tree.NewTree(5)
	memTree.Add("root", "n1", "machine learning with python")
	memTree.Add("root", "n2", "distributed systems design")

	w := RetrievalWeights{FTS5: 0, Vector: 0, Keyword: 0, Tree: 1.0}
	retriever := NewMultiStrategyRetriever(memStore, memTree, nil, w, nil)

	results, err := retriever.Search(context.Background(), "python", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least 1 tree result")
	}
}

func TestMultiStrategyRetriever_Search_Keyword(t *testing.T) {
	db := newTestDB(t)
	memStore, _ := store.NewStore(db)
	memStore.Insert(store.MemoryChunk{Source: "test", Content: "unrelated text"})
	memStore.Insert(store.MemoryChunk{Source: "test", Content: "this contains the query keyword"})
	memTree := tree.NewTree(5)

	w := RetrievalWeights{FTS5: 0, Vector: 0, Keyword: 1.0, Tree: 0}
	retriever := NewMultiStrategyRetriever(memStore, memTree, nil, w, nil)

	results, err := retriever.Search(context.Background(), "query keyword", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected keyword match")
	}
}

func TestFormatScoredResults(t *testing.T) {
	results := []ScoredChunk{
		{Chunk: store.MemoryChunk{Summary: "Result 1", Content: "Content one"}, Score: 0.95, Signals: map[string]float64{"fts5": 0.95}},
	}
	out := FormatScoredResults(results)
	if out == "" {
		t.Error("expected non-empty formatted output")
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return db
}
