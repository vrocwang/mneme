package capability

import (
	"strings"
	"testing"
)

// fakePersister records Persist/Remove calls for assertion in tests.
type fakePersister struct {
	persisted  []*PersistedMCPServer
	removed    []string
	persistErr error
}

func (f *fakePersister) PersistMCPServer(srv *PersistedMCPServer) error {
	if f.persistErr != nil {
		return f.persistErr
	}
	f.persisted = append(f.persisted, srv)
	return nil
}

func (f *fakePersister) RemoveMCPServer(serverID string) error {
	f.removed = append(f.removed, serverID)
	return nil
}

// TestCapabilityRPC_AddMCPServer_ConnectFailureDoesNotPersist verifies that
// when the MCP handshake fails, AddMCPServer returns an error and does NOT
// persist the server (so a broken server won't be reconnected after restart).
func TestCapabilityRPC_AddMCPServer_ConnectFailureDoesNotPersist(t *testing.T) {
	reg := NewCapabilityRegistry()
	rpc := NewCapabilityRPC(reg)
	fp := &fakePersister{}
	rpc.SetMCPPersister(fp)

	// A nonexistent command makes NewStdio fail fast at cmd.Start().
	err := rpc.AddMCPServer("broken", "stdio", "/nonexistent-binary-xyz", "", nil)
	if err == nil {
		t.Fatal("expected error for unconnectable MCP server, got nil")
	}
	if len(fp.persisted) != 0 {
		t.Fatalf("expected 0 persist calls on connect failure, got %d", len(fp.persisted))
	}
	// The failed set must be rolled back so a retry is not blocked by a
	// stale "already registered" entry.
	if _, ok := reg.GetSet("mcp:broken"); ok {
		t.Fatal("expected failed-connect set to be rolled back, but it remains in the registry")
	}
	// A retry with the same name must reach ConnectMCPServer again (failing
	// with "connect failed"), not be rejected by AddSet as a duplicate.
	err = rpc.AddMCPServer("broken", "stdio", "/nonexistent-binary-xyz", "", nil)
	if err == nil {
		t.Fatal("expected retry to also fail (bogus command), got nil")
	}
	if strings.Contains(err.Error(), "already registered") {
		t.Fatalf("retry was blocked by stale set instead of reaching connect: %v", err)
	}
}

// TestCapabilityRPC_RemoveMCPServer_IdempotentWhenSetMissing verifies that
// removing a server that exists only in the DB (not in the in-memory registry,
// e.g. after a restart that failed to reconnect) still cleans up the persisted
// record and does NOT return a spurious error to the caller.
func TestCapabilityRPC_RemoveMCPServer_IdempotentWhenSetMissing(t *testing.T) {
	reg := NewCapabilityRegistry()
	rpc := NewCapabilityRPC(reg)
	fp := &fakePersister{}
	rpc.SetMCPPersister(fp)

	// No set is added to the registry, simulating a post-restart state where
	// the server exists in the DB but was not reconnected into memory.
	if err := rpc.RemoveMCPServer("orphan"); err != nil {
		t.Fatalf("expected nil error for idempotent remove, got %v", err)
	}
	if len(fp.removed) != 1 || fp.removed[0] != "mcp:orphan" {
		t.Fatalf("expected persister to remove 'mcp:orphan', got %v", fp.removed)
	}
}

// TestCapabilityRPC_RemoveMCPServer_RemovesFromRegistryAndPersister verifies
// the normal remove path: the in-memory set is removed, the persister is
// invoked, and no error is returned.
func TestCapabilityRPC_RemoveMCPServer_RemovesFromRegistryAndPersister(t *testing.T) {
	reg := NewCapabilityRegistry()
	rpc := NewCapabilityRPC(reg)
	fp := &fakePersister{}
	rpc.SetMCPPersister(fp)

	// Seed the registry with a set (simulating a connected server).
	if err := reg.AddSet(&CapabilitySet{
		ID: "mcp:seed", Name: "seed", Kind: KindMCPServer, Enabled: true,
	}); err != nil {
		t.Fatalf("AddSet: %v", err)
	}

	if err := rpc.RemoveMCPServer("seed"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if _, ok := reg.GetSet("mcp:seed"); ok {
		t.Error("expected set to be removed from registry")
	}
	if len(fp.removed) != 1 || fp.removed[0] != "mcp:seed" {
		t.Fatalf("expected persister to remove 'mcp:seed', got %v", fp.removed)
	}
}

// TestCapabilityRPC_RemoveMCPServer_NoPersister verifies that without a
// persister wired, RemoveMCPServer still removes the in-memory set and
// surfaces the RemoveSet result.
func TestCapabilityRPC_RemoveMCPServer_NoPersister(t *testing.T) {
	reg := NewCapabilityRegistry()
	rpc := NewCapabilityRPC(reg)

	if err := reg.AddSet(&CapabilitySet{
		ID: "mcp:lonely", Name: "lonely", Kind: KindMCPServer, Enabled: true,
	}); err != nil {
		t.Fatalf("AddSet: %v", err)
	}
	if err := rpc.RemoveMCPServer("lonely"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if _, ok := reg.GetSet("mcp:lonely"); ok {
		t.Error("expected set to be removed from registry")
	}
}
