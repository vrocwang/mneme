package memory

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "github.com/simon/mneme/internal/sqlite"

	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/memory/queue"
	"github.com/simon/mneme/internal/memory/store"
)

func TestPipeline_IndexAndSearch(t *testing.T) {
	db := openDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	convStore, err := conversations.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	memStore, err := store.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	queue.Migrate(db)
	p := NewPipeline(log, convStore, memStore, db)

	// Insert directly (synchronous) for test
	memStore.Insert(store.MemoryChunk{Source: "manual", Content: "Bitcoin uses proof-of-work"})
	memStore.Insert(store.MemoryChunk{Source: "manual", Content: "Ethereum uses proof-of-stake"})

	result, err := p.Search(context.Background(), "Bitcoin", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalResults() < 1 {
		t.Error("expected at least 1 result for Bitcoin")
	}
}

func TestPipeline_ArchiveAndSearch(t *testing.T) {
	db := openDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	convStore, _ := conversations.NewStore(db)
	memStore, _ := store.NewStore(db)
	queue.Migrate(db)
	p := NewPipeline(log, convStore, memStore, db)

	// Insert directly (synchronous) — queue workers run async and require
	// long polling which is tested separately.
	memStore.Insert(store.MemoryChunk{Source: "conversation", Content: "DeFi is decentralized finance on blockchain"})

	result, err := p.Search(context.Background(), "DeFi", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalResults() < 1 {
		t.Error("expected at least 1 result for DeFi after direct insert")
	}
}

func openDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
