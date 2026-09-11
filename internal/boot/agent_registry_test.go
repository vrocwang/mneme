package boot

import (
	"testing"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

// TestAgentRegistryAdapterCarriesRoutingFields guards the descriptor→eino
// mapping: the roster's max_iterations and model route must survive the
// capability registry so the eino agent set can apply them. Both were
// previously dropped, leaving the roster's values inert.
func TestAgentRegistryAdapterCarriesRoutingFields(t *testing.T) {
	reg := capability.NewCapabilityRegistry()
	// RegisterAgentSet creates the set and the descriptor for the definition.
	reg.RegisterAgentSet("builtin", capability.KindBuiltin, "Core", &tools.AgentDef{
		ID:            "researcher",
		Name:          "Researcher",
		Description:   "research",
		Tier:          "reasoning",
		MaxIterations: 15,
		Model:         "reasoning",
	})

	adapter := &agentRegistryAdapter{capReg: reg}
	defs := adapter.AgentDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 agent def, got %d", len(defs))
	}
	got := defs[0]
	if got.ID != "researcher" {
		t.Fatalf("agent id = %q, want researcher", got.ID)
	}
	if got.MaxIterations != 15 {
		t.Errorf("max_iterations = %d, want 15", got.MaxIterations)
	}
	if got.Model != "reasoning" {
		t.Errorf("model route = %q, want reasoning", got.Model)
	}
}

// TestAgentRegistryAdapterNilRegistry ensures the adapter is nil-safe, which
// boot relies on when the registry is unavailable.
func TestAgentRegistryAdapterNilRegistry(t *testing.T) {
	adapter := &agentRegistryAdapter{}
	if defs := adapter.AgentDefs(); defs != nil {
		t.Errorf("expected nil defs for nil registry, got %d", len(defs))
	}
}
