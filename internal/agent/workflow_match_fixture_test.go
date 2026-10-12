package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon/mneme/internal/workflow_doc"
)

// TestWorkflowMatchSharedFixture pins the core's ranking to the same fixture the
// workflows extension asserts against (extensions/workflows/match_fixture_test.go).
// The core decides which workflows are injected into the prompt; the extension's
// workflow_match tool decides which one it names. A behavioural divergence
// between the two would have the model told about one workflow and the tool
// recommend another, so both sides must agree on the fixture's winner.
func TestWorkflowMatchSharedFixture(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "workflow_match")
	raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatalf("read fixture expectation: %v", err)
	}
	var want struct {
		Task   string `json:"task"`
		Winner string `json:"winner"`
		Why    string `json:"why"`
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("parse fixture expectation: %v", err)
	}

	wi := NewWorkflowInjector(filepath.Join(dir, "workflows"), "")
	wi.Load()
	if len(wi.workflows) == 0 {
		t.Fatal("fixture discovered no workflows")
	}

	matches := wi.Match(context.Background(), want.Task)
	if len(matches) == 0 {
		t.Fatalf("Match(%q) found nothing; want %s (%s)", want.Task, want.Winner, want.Why)
	}
	if matches[0].DirName != want.Winner {
		t.Fatalf("winner = %q, want %q (%s)", matches[0].DirName, want.Winner, want.Why)
	}
	if len(matches) > 3 {
		t.Fatalf("matched %d workflows, want at most 3", len(matches))
	}
	// An unrelated task must not match anything.
	if got := wi.Match(context.Background(), "water the office plants"); len(got) != 0 {
		t.Fatalf("unrelated task matched %d workflows: %v", len(got), got)
	}

	// The top three must be ordered best-first, which is what the extension's
	// ranking promises as well.
	for i := 1; i < len(matches); i++ {
		prev := workflowScore(want.Task, matches[i-1])
		cur := workflowScore(want.Task, matches[i])
		if cur > prev {
			t.Fatalf("matches not sorted by score: %s (%d) before %s (%d)",
				matches[i-1].DirName, prev, matches[i].DirName, cur)
		}
	}
}

// workflowScore mirrors the injector's scoring so the ordering assertion above
// does not depend on the private comparison inside Match.
func workflowScore(task string, wf *workflow_doc.Workflow) int {
	taskLower := strings.ToLower(task)
	score := workflow_doc.WordOverlap(taskLower, strings.ToLower(wf.Frontmatter["when_to_use"]))
	score += workflow_doc.WordOverlap(taskLower, strings.ToLower(wf.Name))
	score += workflow_doc.WordOverlap(taskLower, strings.ToLower(wf.Description))
	return score
}
