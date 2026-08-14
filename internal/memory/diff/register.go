package diff

import "github.com/simon/mneme/internal/capability"

// RegisterMemoryDiffTools registers the memory_changes tool under the
// "memory" set.
func RegisterMemoryDiffTools(reg *capability.CapabilityRegistry, store *Store) {
	if reg != nil && store != nil {
		reg.EnsureSet(&capability.CapabilitySet{
			ID:      "memory",
			Name:    "Memory",
			Kind:    capability.KindBuiltin,
			Enabled: true,
		})
		reg.RegisterTool("memory", NewMemoryChangesTool(store))
	}
}
