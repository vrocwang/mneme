package capability

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// policyFakePersister records persistence calls so the readiness tests can
// prove a mutation never reaches the persister while the registry is unset.
type policyFakePersister struct {
	persisted int
	removed   int
}

func (f *policyFakePersister) PersistMCPServer(*PersistedMCPServer) error {
	f.persisted++
	return nil
}

func (f *policyFakePersister) RemoveMCPServer(string) error {
	f.removed++
	return nil
}

// TestCapabilityRPC_NilRegistryMutationsError is the SET 2.10 policy test for
// the mutating half: before the registry exists, a mutation must report an
// explicit error instead of silently succeeding.
func TestCapabilityRPC_NilRegistryMutationsError(t *testing.T) {
	mutations := []struct {
		name string
		call func(rpc *CapabilityRPC) error
	}{
		{"AddMCPServer", func(rpc *CapabilityRPC) error {
			return rpc.AddMCPServer("server", "stdio", "cmd", "", nil)
		}},
		{"RemoveMCPServer", func(rpc *CapabilityRPC) error {
			return rpc.RemoveMCPServer("server")
		}},
		{"ConnectMCPServer", func(rpc *CapabilityRPC) error {
			return rpc.ConnectMCPServer("server")
		}},
		{"DisconnectMCPServer", func(rpc *CapabilityRPC) error {
			return rpc.DisconnectMCPServer("server")
		}},
		{"InstallExtension", func(rpc *CapabilityRPC) error {
			return rpc.InstallExtension("/nonexistent/package")
		}},
		{"UninstallExtension", func(rpc *CapabilityRPC) error {
			return rpc.UninstallExtension("category", "name")
		}},
		{"InstallSkill", func(rpc *CapabilityRPC) error {
			return rpc.InstallSkill("some-source")
		}},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			rpc := NewCapabilityRPC(nil)
			fp := &policyFakePersister{}
			rpc.SetMCPPersister(fp)

			err := m.call(rpc)
			if err == nil {
				t.Fatalf("%s with a nil registry returned nil; a dropped mutation must not report success", m.name)
			}
			if !errors.Is(err, errRegistryNotReady) {
				t.Errorf("%s error = %v, want errRegistryNotReady", m.name, err)
			}
			if fp.persisted != 0 || fp.removed != 0 {
				t.Errorf("%s touched the persister despite the registry being unset: %+v", m.name, fp)
			}
		})
	}
}

// TestCapabilityRPC_NilRegistryQueriesReturnEmpty is the read-only half of the
// same policy: queries degrade to empty results rather than panicking or
// erroring.
func TestCapabilityRPC_NilRegistryQueriesReturnEmpty(t *testing.T) {
	rpc := NewCapabilityRPC(nil)

	if got := rpc.ListSets(); got != nil {
		t.Errorf("ListSets() = %v, want nil", got)
	}
	if set, ok := rpc.GetSet("anything"); set != nil || ok {
		t.Errorf("GetSet() = (%v, %v), want (nil, false)", set, ok)
	}
	if got := rpc.ListAllTools(); got != nil {
		t.Errorf("ListAllTools() = %v, want nil", got)
	}
	if got := rpc.GetToolSchema("anything"); got != nil {
		t.Errorf("GetToolSchema() = %v, want nil", got)
	}
	if got := rpc.ListAllAgents(); got != nil {
		t.Errorf("ListAllAgents() = %v, want nil", got)
	}
	if got := rpc.ListExtensions(); got != nil {
		t.Errorf("ListExtensions() = %v, want nil", got)
	}

	diag := rpc.GetToolDiagnostics()
	if ok, _ := diag["ok"].(bool); ok {
		t.Errorf("GetToolDiagnostics()[ok] = true with a nil registry: %+v", diag)
	}
	// The transport split must be present (and zero) even in the not-ready shape
	// so the frontend can read the same keys unconditionally.
	for _, key := range []string{"totalTools", "enabledTools", "mcpStdioTools", "mcpHTTPTools"} {
		v, present := diag[key]
		if !present {
			t.Errorf("GetToolDiagnostics() is missing key %q in the not-ready shape", key)
			continue
		}
		if n, ok := v.(int); !ok || n != 0 {
			t.Errorf("GetToolDiagnostics()[%q] = %v, want 0", key, v)
		}
	}
}

// TestCapabilityRPC_ReadinessPolicyIsUniform pins the policy decision itself:
// every exported method is classified as a query (empty result with a nil
// registry), a registry-independent setter (Set*), or a mutation (explicit
// error). A new method added without a decision fails here, so the policy
// cannot silently drift back to a mix.
func TestCapabilityRPC_ReadinessPolicyIsUniform(t *testing.T) {
	// Mutating methods: must return errRegistryNotReady with a nil registry.
	mutating := map[string]bool{
		"AddMCPServer": true, "RemoveMCPServer": true, "ConnectMCPServer": true,
		"DisconnectMCPServer": true, "InstallExtension": true,
		"UninstallExtension": true, "InstallSkill": true,
	}
	// Read-only queries: must return a zero value with a nil registry.
	queries := map[string]bool{
		"ListSets": true, "GetSet": true, "ListAllTools": true, "GetToolSchema": true,
		"ListAllAgents": true, "GetToolDiagnostics": true, "ListExtensions": true,
	}

	typ := reflect.TypeOf(&CapabilityRPC{})
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		switch {
		case queries[name], mutating[name]:
		case strings.HasPrefix(name, "Set"):
			// Registry-independent setter (SetRegistry, SetMCPPersister, ...).
		default:
			t.Errorf("exported method %s is not classified by the readiness policy test; "+
				"decide whether it is a mutation (error when the registry is unset) or a query (empty result)", name)
		}
	}

	// Guard against a stale entry: every classified name must exist.
	for _, group := range []map[string]bool{mutating, queries} {
		for name := range group {
			if _, ok := typ.MethodByName(name); !ok {
				t.Errorf("classified method %s no longer exists on CapabilityRPC", name)
			}
		}
	}
}
