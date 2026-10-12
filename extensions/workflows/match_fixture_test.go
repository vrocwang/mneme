package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// matchFixture is the expectation shared with the core prompt injector
// (internal/agent/workflow_inject.go). Both sides assert the same winner for the
// same fixture so the tool and the injected prompt cannot disagree about which
// workflow applies.
type matchFixture struct {
	Task   string `json:"task"`
	Winner string `json:"winner"`
	Why    string `json:"why"`
}

// TestMatchWorkflowsSharedFixture pins the extension's ranking to the shared
// fixture. If the core's scoring changes and this does not, the test that runs
// against the same files in internal/agent fails instead of the two silently
// drifting apart.
func TestMatchWorkflowsSharedFixture(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "workflow_match")
	raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatalf("read fixture expectation: %v", err)
	}
	var want matchFixture
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("parse fixture expectation: %v", err)
	}

	workflows := DiscoverWorkflows(filepath.Join(dir, "workflows"), "")
	if len(workflows) == 0 {
		t.Fatal("fixture discovered no workflows")
	}

	ranked := MatchWorkflows(want.Task, workflows)
	if len(ranked) == 0 {
		t.Fatalf("MatchWorkflows(%q) found nothing; want %s (%s)", want.Task, want.Winner, want.Why)
	}
	if ranked[0].DirName != want.Winner {
		t.Fatalf("winner = %q, want %q (%s)", ranked[0].DirName, want.Winner, want.Why)
	}
	// BestMatch must agree with the ranking — the tool uses it directly.
	best := BestMatch(want.Task, workflows)
	if best == nil || best.DirName != want.Winner {
		t.Fatalf("BestMatch = %v, want %q", best, want.Winner)
	}
	// The core keeps at most three; the tool must not rank more than that.
	if len(ranked) > 3 {
		t.Fatalf("ranked %d workflows, want at most 3", len(ranked))
	}
	// An unrelated task must not match anything.
	if got := MatchWorkflows("water the office plants", workflows); len(got) != 0 {
		t.Fatalf("unrelated task matched %d workflows: %v", len(got), got)
	}
}
