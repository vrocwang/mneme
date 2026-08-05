package cost

import (
	"fmt"
	"sync"
)

// Usage tracks token usage per model.
type Usage struct {
	Model        string
	InputTokens  int
	OutputTokens int
}

// Tracker tracks LLM usage costs.
type Tracker struct {
	mu     sync.RWMutex
	usage  map[string]*Usage // keyed by model
	budget int               // monthly budget in cents
}

func NewTracker(budgetCents int) *Tracker {
	return &Tracker{
		usage:  make(map[string]*Usage),
		budget: budgetCents,
	}
}

// Record tracks token usage for a model call.
func (t *Tracker) Record(model string, inputTokens, outputTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	u, ok := t.usage[model]
	if !ok {
		u = &Usage{Model: model}
		t.usage[model] = u
	}
	u.InputTokens += inputTokens
	u.OutputTokens += outputTokens
}

// TotalCost calculates the estimated total cost in cents.
func (t *Tracker) TotalCost() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	total := 0
	for _, u := range t.usage {
		total += estimateCost(u.Model, u.InputTokens, u.OutputTokens)
	}
	return total
}

// BudgetUsed returns the percentage of budget consumed.
func (t *Tracker) BudgetUsed() float64 {
	if t.budget == 0 {
		return 0
	}
	return float64(t.TotalCost()) / float64(t.budget) * 100
}

// Overview returns a human-readable usage summary.
func (t *Tracker) Overview() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.usage) == 0 {
		return "No usage tracked yet."
	}

	var out string
	out += fmt.Sprintf("Total: $%.2f", float64(t.TotalCost())/100)
	if t.budget > 0 {
		out += fmt.Sprintf(" (%.1f%% of $%.2f budget)", t.BudgetUsed(), float64(t.budget)/100)
	}
	out += "\n\nBy model:\n"

	for _, u := range t.usage {
		cost := estimateCost(u.Model, u.InputTokens, u.OutputTokens)
		out += fmt.Sprintf("- %s: %d in / %d out tokens = $%.4f\n",
			u.Model, u.InputTokens, u.OutputTokens, float64(cost)/100)
	}
	return out
}

func estimateCost(model string, inputTok, outputTok int) int {
	// Approximate costs per 1K tokens (in cents * 1000 for integer math)
	switch {
	case contains(model, "gpt-4o"):
		return (inputTok*5 + outputTok*15) / 1000
	case contains(model, "claude-sonnet"):
		return (inputTok*3 + outputTok*15) / 1000
	case contains(model, "claude-haiku"):
		return (inputTok*1 + outputTok*5) / 1000
	case contains(model, "ollama"), contains(model, "lmstudio"), contains(model, "local"):
		return 0
	default:
		return (inputTok*3 + outputTok*10) / 1000
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
