package about_app

import (
	"testing"
)

func TestDirectoryRegister(t *testing.T) {
	d := NewDirectory()

	d.Register(Capability{Kind: "agent", Name: "general", Description: "General agent"})
	d.Register(Capability{Kind: "tool", Name: "read_file", Description: "Read file"})
	d.Register(Capability{Kind: "rpc", Name: "agent.chat", Description: "Chat RPC"})

	if d.Count() != 3 {
		t.Fatalf("expected 3 capabilities, got %d", d.Count())
	}
}

func TestDirectoryListByKind(t *testing.T) {
	d := NewDirectory()
	d.Register(Capability{Kind: "agent", Name: "coder", Description: "Coder"})
	d.Register(Capability{Kind: "agent", Name: "critic", Description: "Critic"})
	d.Register(Capability{Kind: "tool", Name: "shell", Description: "Shell"})

	agents := d.ListByKind("agent")
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	tools := d.ListByKind("tool")
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
}

func TestDirectoryUnregister(t *testing.T) {
	d := NewDirectory()
	d.Register(Capability{Kind: "tool", Name: "delete_me", Description: "Gone"})
	if d.Count() != 1 {
		t.Fatal("expected 1")
	}
	d.Unregister("tool", "delete_me")
	if d.Count() != 0 {
		t.Fatal("expected 0 after unregister")
	}
}

func TestDirectorySnapshot(t *testing.T) {
	d := NewDirectory()
	d.Register(Capability{Kind: "agent", Name: "test", Description: "Test agent"})

	snap := d.Snapshot()
	if snap["total_count"].(int) != 1 {
		t.Fatalf("expected 1 in snapshot, got %v", snap["total_count"])
	}
}

func TestDirectoryCountByKind(t *testing.T) {
	d := NewDirectory()
	d.Register(Capability{Kind: "agent", Name: "a1", Description: "A1"})
	d.Register(Capability{Kind: "agent", Name: "a2", Description: "A2"})
	d.Register(Capability{Kind: "tool", Name: "t1", Description: "T1"})

	counts := d.CountByKind()
	if counts["agent"] != 2 || counts["tool"] != 1 {
		t.Fatalf("unexpected counts: %v", counts)
	}
}
