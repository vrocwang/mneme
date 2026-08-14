package memory

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/memory/queue"
	"github.com/simon/mneme/internal/memory/store"
)

// TestPipeline_ExtractAtoms_Layered verifies the L0→L1 extraction path: a
// conversation is atomized into L1 atoms via the heuristic fallback (no LLM in
// tests), each traced back to its source thread.
func TestPipeline_ExtractAtoms_Layered(t *testing.T) {
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
	if p.layered == nil {
		t.Fatal("layered store should be created when db is provided")
	}

	// Add two substantive user messages to a thread.
	convStore.EnsureThread("t1", "hi")
	if err := convStore.AddMessage("t1", "user", "The user prefers Rust over Python for CLI tools because of its performance."); err != nil {
		t.Fatal(err)
	}
	if err := convStore.AddMessage("t1", "user", "The user lives in Beijing and works remotely for a startup."); err != nil {
		t.Fatal(err)
	}

	msgs, err := convStore.GetMessages("t1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected messages")
	}

	// Build the conversation doc the same way handleArchive does, then run
	// the layered atom extraction.
	var doc string
	for _, m := range msgs {
		doc += fmt.Sprintf("[%s]: %s\n", m.Role, m.Content)
	}
	p.extractAtoms(context.Background(), "t1", doc, nil)

	atoms, err := p.layered.ListAtomsRecent(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) == 0 {
		t.Fatal("expected at least one L1 atom after extraction")
	}
	// Every atom must carry an L0 ref (thread traceability).
	for _, a := range atoms {
		if len(a.Refs) == 0 || a.Refs[0].ThreadID != "t1" {
			t.Errorf("atom %d missing thread ref: %+v", a.ID, a.Refs)
		}
	}
}

// TestPipeline_AggregateScenarios_Layered verifies L1→L2 aggregation: once
// enough atoms accumulate, they are rolled into a scenario and the atoms are
// marked as aggregated.
func TestPipeline_AggregateScenarios_Layered(t *testing.T) {
	db := openDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	convStore, _ := conversations.NewStore(db)
	memStore, _ := store.NewStore(db)
	queue.Migrate(db)
	p := NewPipeline(log, convStore, memStore, db)

	ctx := context.Background()
	// Insert enough atoms to cross the aggregation threshold.
	for i := 0; i < minAtomsPerScenario; i++ {
		if _, err := p.layered.InsertAtom(ctx, store.Atom{
			Content: "Fact number " + string(rune('a'+i)) + " about the user's technology preferences",
			Source:  "conversation",
		}); err != nil {
			t.Fatal(err)
		}
	}

	p.aggregateScenarios(ctx)

	scenarios, err := p.layered.ListScenariosRecent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("expected at least one L2 scenario after aggregation")
	}

	// The aggregated atoms must now carry the scenario ID (drill-down edge).
	first := scenarios[0]
	atoms, err := p.layered.ListAtomsByIDs(ctx, first.AtomIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) == 0 {
		t.Fatal("expected atoms under the scenario")
	}
	for _, a := range atoms {
		if a.ScenarioID != first.ID {
			t.Errorf("atom %d not marked with scenario %d (got %d)", a.ID, first.ID, a.ScenarioID)
		}
	}
}
