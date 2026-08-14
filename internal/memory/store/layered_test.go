package store

import (
	"context"
	"testing"
)

func TestLayeredStore_InsertAndSearchAtoms(t *testing.T) {
	db := openDB(t)
	ls, err := NewLayeredStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := ls.InsertAtom(ctx, Atom{
		Content: "The user prefers Rust over Python for CLI tools",
		Source:  "conversation",
		Taint:   TaintInternal,
		Refs:    []AtomRef{{ThreadID: "t1", MessageID: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ls.InsertAtom(ctx, Atom{
		Content: "The user lives in Beijing",
		Source:  "conversation",
		Taint:   TaintInternal,
	}); err != nil {
		t.Fatal(err)
	}

	results, err := ls.SearchAtoms(ctx, "Rust", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 Rust atom, got %d", len(results))
	}
	if results[0].Content != "The user prefers Rust over Python for CLI tools" {
		t.Errorf("unexpected content: %q", results[0].Content)
	}
	// Refs round-trip for drill-down.
	if len(results[0].Refs) != 1 || results[0].Refs[0].ThreadID != "t1" {
		t.Errorf("refs not preserved: %+v", results[0].Refs)
	}
}

func TestLayeredStore_ScenarioDrillDown(t *testing.T) {
	db := openDB(t)
	ls, err := NewLayeredStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	id1, _ := ls.InsertAtom(ctx, Atom{Content: "Prefers Go for backend services", Source: "conversation"})
	id2, _ := ls.InsertAtom(ctx, Atom{Content: "Uses PostgreSQL for persistence", Source: "conversation"})

	if _, err := ls.UpsertScenario(ctx, Scenario{
		Content: "Backend technology stack preferences",
		AtomIDs: []int64{id1, id2},
	}); err != nil {
		t.Fatal(err)
	}

	scs, err := ls.ListScenariosRecent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(scs) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(scs))
	}
	if len(scs[0].AtomIDs) != 2 {
		t.Fatalf("expected 2 atom ids, got %v", scs[0].AtomIDs)
	}

	// Drill down: scenario -> atoms.
	atoms, err := ls.ListAtomsByIDs(ctx, scs[0].AtomIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms from drill-down, got %d", len(atoms))
	}
}

func TestLayeredStore_MigrateChunksToAtoms(t *testing.T) {
	db := openDB(t)
	legacy, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Insert(MemoryChunk{Source: "conversation", Content: "legacy fact one", Summary: "s1"})
	legacy.Insert(MemoryChunk{Source: "conversation", Content: "legacy fact two", Summary: "s2"})

	ls, err := NewLayeredStore(db)
	if err != nil {
		t.Fatal(err)
	}
	n, err := ls.MigrateChunksToAtoms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 migrated atoms, got %d", n)
	}

	atoms, err := ls.ListAtomsRecent(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms after migration, got %d", len(atoms))
	}

	// Migration is non-destructive: legacy table intact.
	legacyRecent, err := legacy.ListRecent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyRecent) != 2 {
		t.Fatalf("legacy chunks should be preserved, got %d", len(legacyRecent))
	}
}
