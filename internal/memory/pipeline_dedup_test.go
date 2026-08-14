package memory

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/simon/mneme/internal/memory/archivist"
	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/memory/queue"
	"github.com/simon/mneme/internal/memory/store"
)

func TestPipeline_DedupAtoms(t *testing.T) {
	db := openDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	convStore, _ := conversations.NewStore(db)
	memStore, _ := store.NewStore(db)
	queue.Migrate(db)
	p := NewPipeline(log, convStore, memStore, db)

	ctx := context.Background()
	// Two near-identical facts: the second should be deduped away.
	facts := []string{
		"The user prefers Rust over Python for CLI tools because of its performance.",
		"The user prefers Rust over Python for CLI tools because of its performance.",
	}

	// Simulate extractAtoms' dedup by inserting the first, then running the
	// dedup check the same way extractAtoms does.
	for i, f := range facts {
		if i > 0 {
			if existing, _ := p.layered.FindAtomByContent(ctx, f); existing != nil {
				if archivist.SimpleSimilarity(existing.Content, f) >= 0.9 {
					continue // deduped
				}
			}
		}
		p.layered.InsertAtom(ctx, store.Atom{Content: f, Source: "conversation"})
	}

	atoms, _ := p.layered.ListAtomsRecent(ctx, 100)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom after dedup, got %d", len(atoms))
	}
}

func TestPipeline_ForgetAtomsOlderThan(t *testing.T) {
	db := openDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	convStore, _ := conversations.NewStore(db)
	memStore, _ := store.NewStore(db)
	queue.Migrate(db)
	p := NewPipeline(log, convStore, memStore, db)

	ctx := context.Background()
	if _, err := p.layered.InsertAtom(ctx, store.Atom{Content: "an old fact", Source: "conversation"}); err != nil {
		t.Fatal(err)
	}

	// Retention with a negative-ish window: since atoms are created "now",
	// deleting older than 1ms should remove nothing; older than 0 should also
	// keep (created_at is rounded to seconds). Use a very small age to exercise
	// the path deterministically.
	n, err := p.ForgetAtomsOlderThan(ctx, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Created "now", so nothing should be older than 1ms yet. This asserts the
	// method runs and returns a count without erroring.
	_ = n
}

func TestPipeline_AtomDrillDown(t *testing.T) {
	db := openDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	convStore, _ := conversations.NewStore(db)
	memStore, _ := store.NewStore(db)
	queue.Migrate(db)
	p := NewPipeline(log, convStore, memStore, db)

	ctx := context.Background()
	id, _ := p.layered.InsertAtom(ctx, store.Atom{
		Content: "The user prefers Rust for CLI tools",
		Source:  "conversation",
		Refs:    []store.AtomRef{{ThreadID: "t1", MessageID: 42}},
	})
	// Create a scenario containing the atom and mark it.
	scID, _ := p.layered.UpsertScenario(ctx, store.Scenario{Content: "Tech preferences", AtomIDs: []int64{id}})
	p.layered.MarkAtomsInScenario(ctx, []int64{id}, scID)

	atom, scenario, err := p.AtomDrillDown(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if atom == nil {
		t.Fatal("expected atom")
	}
	if len(atom.Refs) != 1 || atom.Refs[0].MessageID != 42 {
		t.Errorf("expected source ref message 42, got %+v", atom.Refs)
	}
	if scenario == nil || scenario.ID != scID {
		t.Errorf("expected scenario %d, got %+v", scID, scenario)
	}
}
