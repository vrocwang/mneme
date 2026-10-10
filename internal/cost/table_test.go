package cost

import "testing"

// tokenSample is the token count used for every rate assertion below: 1000
// input + 2000 output tokens. In this table's integer unit
// (tokens × USD-per-1M / 1000) the expected cents are (inPrice + 2×outPrice).
const (
	sampleIn  = 1000
	sampleOut = 2000
)

// TestEstimateCost_FamilyTable is the SET 1.8 rate table: every model family
// internal/config can synthesize from its provider env vars resolves to its
// intended published rate, and nothing falls through to the default by accident.
//
// Expected values are literal cents for the sample above, computed from the
// list price in the comment (1000×in + 2000×out per-1M dollars → cents).
func TestEstimateCost_FamilyTable(t *testing.T) {
	tests := []struct {
		model    string
		want     int
		price    string
		explicit bool // true = own table entry; false = inherits a family entry
	}{
		// Synthesized from OPENAI_API_KEY (internal/config.mergeEnvOverrides).
		{model: "gpt-4o", want: 35, price: "$5/1M in, $15/1M out", explicit: true},
		// Synthesized alongside gpt-4o; inherits the gpt-4o family rate, as it
		// did before the table was extracted.
		{model: "gpt-4o-mini", want: 35, price: "gpt-4o family rate"},
		// Synthesized from ANTHROPIC_API_KEY.
		{model: "claude-sonnet-4-6", want: 33, price: "$3/1M in, $15/1M out", explicit: true},
		{model: "claude-haiku-4-5", want: 11, price: "$1/1M in, $5/1M out", explicit: true},
		// Synthesized from DEEPSEEK_API_KEY. Both names alias the same upstream
		// model and bill at the Flash price ($0.30/1M in peak cache-miss,
		// $1.20/1M out peak): (1000×0.30 + 2000×1.20)/1000 = 2.7 → 2.
		{model: "deepseek-chat", want: 2, price: "$0.30/1M in, $1.20/1M out", explicit: true},
		{model: "deepseek-reasoner", want: 2, price: "$0.30/1M in, $1.20/1M out", explicit: true},
		// Unmatched families book the conservative default ($3/1M in, $10/1M out).
		{model: "some-unknown-model", want: 23, price: "default $3/1M in, $10/1M out"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := estimateCost(tt.model, sampleIn, sampleOut); got != tt.want {
				t.Errorf("estimateCost(%q) = %d cents, want %d (%s)", tt.model, got, tt.want, tt.price)
			}
		})
	}
}

// TestEstimateCost_LocalModelsAreFree pins the explicit zero-cost set: local
// runtimes and loopback endpoints never book spend, even though the same model
// name served remotely would match a paid family (deepseek-r1 is local-only).
func TestEstimateCost_LocalModelsAreFree(t *testing.T) {
	locals := []string{
		"ollama/llama3",
		"ollama/deepseek-chat",
		"lmstudio/qwen2.5",
		"local/llama3",
		"localhost:1234/model",
		"http://127.0.0.1:11434/v1",
		"deepseek-r1",
		"llama.cpp:model",
		"vllm/qwen2.5",
	}
	for _, model := range locals {
		if got := estimateCost(model, 1_000_000, 1_000_000); got != 0 {
			t.Errorf("estimateCost(%q) = %d cents, want 0 (locally hosted)", model, got)
		}
		if !isLocalModel(model) {
			t.Errorf("isLocalModel(%q) = false, want true", model)
		}
	}

	if isLocalModel("gpt-4o") || isLocalModel("deepseek-chat") {
		t.Error("hosted model names must not be classified as local")
	}
}

// TestEstimateCost_ConfigSynthesizedFamiliesAreCovered ties the price table to
// the roster internal/config actually produces.
func TestEstimateCost_ConfigSynthesizedFamiliesAreCovered(t *testing.T) {
	const defaultCents = 23 // the default fallback for the sample tokens

	// Families upserted by config.mergeEnvOverrides from provider env keys must
	// each hit their own entry — never the fallback.
	for _, model := range []string{
		"gpt-4o", "gpt-4o-mini",
		"claude-sonnet-4-6", "claude-haiku-4-5",
		"deepseek-chat", "deepseek-reasoner",
	} {
		got := estimateCost(model, sampleIn, sampleOut)
		if got == 0 {
			t.Errorf("%q booked 0 cents, want a paid rate", model)
		}
		if got == defaultCents {
			t.Errorf("%q falls through to the default rate; add it to modelPriceFamilies", model)
		}
	}

	// The default config's ollama roster. deepseek-r1 is the open-weights tag
	// (local runtimes only), so it is explicitly zero-cost — without that
	// marker it would sit right next to the new deepseek entries.
	if got := estimateCost("deepseek-r1", sampleIn, sampleOut); got != 0 {
		t.Errorf("deepseek-r1 booked %d cents, want 0 (locally hosted open weights)", got)
	}

	// llama3 and qwen2.5 are also listed under the ollama provider, but the
	// model label alone cannot prove locality: paid gateways host the same
	// open-weights names. They therefore keep the conservative default rate on
	// purpose, which is also why the tracker cannot be exact without the
	// provider endpoint (see the TurnResult.Model gap in the report).
	for _, model := range []string{"llama3", "qwen2.5"} {
		if got := estimateCost(model, sampleIn, sampleOut); got == 0 {
			t.Errorf("%q booked 0 cents; bare open-weights names must not be assumed local", model)
		}
	}
}

// TestPriceCents_MatchesHistoricalIntegerFormula guards the extraction: for
// whole-dollar prices the micro-dollar math must be bit-identical to the
// original `(in*X + out*Y) / 1000` expression.
func TestPriceCents_MatchesHistoricalIntegerFormula(t *testing.T) {
	cases := []struct {
		in, out int
		x, y    int64
	}{
		{1000, 500, 5, 15},
		{123456, 78910, 3, 15},
		{1, 1, 1, 5},
		{0, 999, 3, 10},
		{1_000_000, 1_000_000, 5, 15},
	}
	for _, c := range cases {
		p := modelPrice{inMicro: c.x * 1_000_000, outMicro: c.y * 1_000_000}
		want := (c.in*int(c.x) + c.out*int(c.y)) / 1000
		if got := priceCents(p, c.in, c.out); got != want {
			t.Errorf("priceCents(%d, %d) = %d, want %d (historical formula)", c.in, c.out, got, want)
		}
	}
}
