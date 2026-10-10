package bundle

import (
	"testing"

	agenttoml "github.com/simon/mneme/internal/agent/toml"
)

// TestBuiltinAgentsRoster loads the embedded roster and asserts the
// invariants the eino agent set depends on.
func TestBuiltinAgentsRoster(t *testing.T) {
	data, err := builtinAgentsTOML.ReadFile("builtin_agents.toml")
	if err != nil {
		t.Fatalf("read embedded roster: %v", err)
	}
	defs, err := agenttoml.LoadAgentsFromEmbedded(string(data))
	if err != nil {
		t.Fatalf("parse embedded roster: %v", err)
	}

	want := map[string]struct {
		tier     string
		hidden   bool
		maxIters int
	}{
		"general":      {tier: "chat", maxIters: 10},
		"orchestrator": {tier: "chat", maxIters: 20},
		"researcher":   {tier: "reasoning", maxIters: 15},
		"coder":        {tier: "worker", maxIters: 15},
		"critic":       {tier: "reasoning", maxIters: 5},
		"planner":      {tier: "reasoning", maxIters: 10},
		"summarizer":   {tier: "worker", hidden: true, maxIters: 3},
		"archivist":    {tier: "worker", hidden: true, maxIters: 5},
		"mcp_setup":    {tier: "worker", maxIters: 10},
		"tool_maker":   {tier: "worker", maxIters: 15},
		"task_manager": {tier: "reasoning", maxIters: 15},
		"tools_agent":  {tier: "worker", hidden: true, maxIters: 15},
	}
	if len(defs) < len(want) {
		t.Fatalf("expected at least %d builtin agents, got %d", len(want), len(defs))
	}

	byID := make(map[string]*struct {
		tier     string
		hidden   bool
		maxIters int
	}, 0)
	_ = byID
	seen := make(map[string]bool)
	for _, def := range defs {
		seen[def.ID] = true
		w, ok := want[def.ID]
		if !ok {
			continue // extra agents allowed
		}
		if def.Tier != w.tier {
			t.Errorf("agent %s: tier = %q, want %q", def.ID, def.Tier, w.tier)
		}
		if def.Hidden != w.hidden {
			t.Errorf("agent %s: hidden = %v, want %v", def.ID, def.Hidden, w.hidden)
		}
		if def.MaxIterations != w.maxIters {
			t.Errorf("agent %s: max_iterations = %d, want %d", def.ID, def.MaxIterations, w.maxIters)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("builtin agent %q missing from roster", id)
		}
	}

	// Orchestrator must keep its delegation set.
	for _, def := range defs {
		if def.ID == "orchestrator" && len(def.SubagentRefs) == 0 {
			t.Error("orchestrator lost its subagent refs")
		}
	}
	// Coder keeps its sandbox mode and allowlist, researcher its model route.
	for _, def := range defs {
		switch def.ID {
		case "coder":
			if def.SandboxMode != "read_write" {
				t.Errorf("coder sandbox_mode = %q, want read_write", def.SandboxMode)
			}
			if len(def.ToolAllowlist) == 0 {
				t.Error("coder lost its tool allowlist")
			}
		case "researcher":
			if def.Model != "reasoning" {
				t.Errorf("researcher model = %q, want reasoning", def.Model)
			}
		case "general":
			if len(def.ToolAllowlist) != 1 || def.ToolAllowlist[0] != "*" {
				t.Error("general should allow all tools")
			}
		}
	}
}
