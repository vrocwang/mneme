package cost

import "testing"

func TestTracker_RecordAndCost(t *testing.T) {
	tr := NewTracker(10000) // $100 budget in cents
	tr.Record("gpt-4o", 1000, 500)
	tr.Record("ollama/llama3", 5000, 3000)

	cost := tr.TotalCost()
	if cost <= 0 {
		t.Error("expected nonzero cost for gpt-4o usage")
	}
	// ollama should contribute 0 cost
	if cost > 1000 {
		t.Errorf("cost seems too high: %d cents", cost)
	}
}

func TestTracker_BudgetUsed(t *testing.T) {
	tr := NewTracker(1000)             // $10 budget
	tr.Record("gpt-4o", 100000, 50000) // expensive

	used := tr.BudgetUsed()
	if used <= 0 {
		t.Error("expected budget to be used")
	}
}

func TestTracker_Overview(t *testing.T) {
	tr := NewTracker(0)
	overview := tr.Overview()
	if overview != "No usage tracked yet." {
		t.Error("expected empty message")
	}

	tr.Record("claude-sonnet-4-6", 1000, 200)
	overview = tr.Overview()
	if overview == "No usage tracked yet." {
		t.Error("expected usage summary")
	}
}
