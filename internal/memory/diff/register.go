package diff

import "github.com/simon/mneme/internal/capability"

// RegisterMemoryDiffTools registers the memory_changes tool.
func RegisterMemoryDiffTools(reg *capability.CapabilityRegistry, store *Store) {
	if reg != nil && store != nil {
		reg.RegisterTool("builtin", NewMemoryChangesTool(store))
	}
}
