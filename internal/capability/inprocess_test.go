package capability

import (
	"context"
	"testing"

	"github.com/simon/mneme/internal/tools"
)

// stubTool is a minimal in-process tools.Tool used to exercise registration.
type stubTool struct{ name string }

func (s *stubTool) Schema() tools.Schema {
	return tools.Schema{Name: s.name, Description: s.name}
}
func (s *stubTool) Execute(context.Context, map[string]interface{}) tools.Result {
	return tools.Result{Success: true}
}

// TestRegisterInProcessSet_Dispose verifies the in-process registration path:
// it registers a set as a single effect, exposes its tools/agents through the
// same registry views as a process-isolated extension, and unwinds cleanly
// (idempotently) on dispose.
func TestRegisterInProcessSet_Dispose(t *testing.T) {
	reg := NewCapabilityRegistry()

	set := &CapabilitySet{ID: "inproc:test", Name: "InProc", Kind: KindBuiltin, Enabled: true}
	disposeFn, err := reg.RegisterInProcessSet(set,
		[]tools.Tool{&stubTool{name: "stub_a"}, &stubTool{name: "stub_b"}},
		[]*tools.AgentDef{{ID: "agent_x", Name: "Agent X", Tier: "chat"}},
	)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if len(reg.ToolNames()) != 2 {
		t.Fatalf("expected 2 tools after register, got %d: %v", len(reg.ToolNames()), reg.ToolNames())
	}
	if _, ok := reg.GetAgent("agent_x"); !ok {
		t.Fatal("expected agent_x to be registered")
	}

	// Unwind — must clear tools, agents, and the set.
	disposeFn()
	if len(reg.ToolNames()) != 0 {
		t.Fatalf("expected 0 tools after dispose, got %v", reg.ToolNames())
	}
	if _, ok := reg.GetAgent("agent_x"); ok {
		t.Fatal("agent_x should be gone after dispose")
	}
	if _, ok := reg.GetSet("inproc:test"); ok {
		t.Fatal("set should be gone after dispose")
	}

	// Idempotent — second dispose is a no-op.
	disposeFn()
}

// TestRegisterInProcessSet_Conflict verifies that a duplicate set registration
// fails without leaving partial state behind.
func TestRegisterInProcessSet_Conflict(t *testing.T) {
	reg := NewCapabilityRegistry()

	set := &CapabilitySet{ID: "inproc:dup", Name: "Dup", Kind: KindBuiltin, Enabled: true}
	if _, err := reg.RegisterInProcessSet(set, []tools.Tool{&stubTool{name: "dup_a"}}, nil); err != nil {
		t.Fatalf("first register: %v", err)
	}

	set2 := &CapabilitySet{ID: "inproc:dup", Name: "Dup", Kind: KindBuiltin, Enabled: true}
	if _, err := reg.RegisterInProcessSet(set2, []tools.Tool{&stubTool{name: "dup_b"}}, nil); err == nil {
		t.Fatal("expected conflict error for duplicate set")
	}

	// First registration must be intact.
	if _, ok := reg.GetTool("dup_a"); !ok {
		t.Fatal("dup_a should still be registered after failed duplicate")
	}
	if _, ok := reg.GetTool("dup_b"); ok {
		t.Fatal("dup_b must not leak from the failed registration")
	}
}
