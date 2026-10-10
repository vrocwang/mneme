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

// modelPrice is a published list price for one model family, in micro-dollars
// per 1M tokens (USD × 1e6). Micro-dollars keep fractional list prices such as
// DeepSeek's $0.30/$1.20 exact without floating point.
type modelPrice struct {
	inMicro  int64
	outMicro int64
}

// modelPriceFamilies maps a model-name substring to its price, evaluated in
// order. It is the model-family price table consumed by estimateCost.
//
// Sources, per 1M tokens:
//   - gpt-4o / claude-sonnet / claude-haiku: launch list prices retained from
//     the original table, at family granularity (https://openai.com/api/pricing/,
//     https://www.anthropic.com/pricing). gpt-4o-mini deliberately inherits
//     the gpt-4o entry, as it did before this table existed.
//   - deepseek-chat / deepseek-reasoner: both names alias the non-thinking and
//     thinking modes of the same upstream DeepSeek model, and are billed at the
//     Flash price. Current published price (checked 2026-10):
//     $0.30/1M input (cache miss, peak) and $1.20/1M output (peak); off-peak is
//     half. Booking the peak cache-miss rate is the conservative upper bound.
//     https://api-docs.deepseek.com/quick_start/pricing/
//     https://api-docs.deepseek.com/updates (2026-09-10 V4.1-Flash,
//     2026-04-24 alias mapping)
//
// internal/config synthesizes exactly these families from the provider API-key
// env vars; if that roster changes, extend this table with it.
var modelPriceFamilies = []struct {
	match string
	price modelPrice
}{
	{"gpt-4o", modelPrice{inMicro: 5_000_000, outMicro: 15_000_000}},         // $5 / $15
	{"claude-sonnet", modelPrice{inMicro: 3_000_000, outMicro: 15_000_000}},  // $3 / $15
	{"claude-haiku", modelPrice{inMicro: 1_000_000, outMicro: 5_000_000}},    // $1 / $5
	{"deepseek-chat", modelPrice{inMicro: 300_000, outMicro: 1_200_000}},     // $0.30 / $1.20
	{"deepseek-reasoner", modelPrice{inMicro: 300_000, outMicro: 1_200_000}}, // $0.30 / $1.20
}

// defaultModelPrice is the fallback for a model that matches no family: it
// books the historical conservative $3 / $10 per 1M rate.
var defaultModelPrice = modelPrice{inMicro: 3_000_000, outMicro: 10_000_000}

// localModelMarkers identify model labels served by a local runtime, which bill
// nothing per token. The tracker only ever sees the model label recorded by the
// agent loop (TurnSnapshot.Model), not the provider base URL, so an endpoint is
// recognized only when the label names a local runtime or embeds a loopback
// host. "deepseek-r1" is included because that tag is the open-weights model
// served through local runtimes — the first-party API exposes the same weights
// under deepseek-reasoner/deepseek-chat — and it is a model the default config
// lists under the local ollama provider.
var localModelMarkers = []string{
	"ollama", "lmstudio", "localhost", "127.0.0.1", "llama.cpp", "llamacpp",
	"vllm", "local", "deepseek-r1",
}

func estimateCost(model string, inputTok, outputTok int) int {
	if isLocalModel(model) {
		return 0
	}
	for _, fam := range modelPriceFamilies {
		if contains(model, fam.match) {
			return priceCents(fam.price, inputTok, outputTok)
		}
	}
	return priceCents(defaultModelPrice, inputTok, outputTok)
}

// isLocalModel reports whether a model label denotes a locally hosted model.
func isLocalModel(model string) bool {
	for _, m := range localModelMarkers {
		if contains(model, m) {
			return true
		}
	}
	return false
}

// priceCents converts token counts and a per-1M-token price into the table's
// integer cost unit: tokens × (USD per 1M tokens) / 1000, truncated. The
// micro-dollar representation makes this exactly equal to the original
// integer formula for whole-dollar prices.
func priceCents(p modelPrice, inputTok, outputTok int) int {
	num := int64(inputTok)*p.inMicro + int64(outputTok)*p.outMicro
	return int(num / 1_000_000_000)
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
