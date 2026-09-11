package capability

import (
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/tools"
)

// RegisterMemoryTools registers memory_search, memory_save, memory_recall,
// and memory_forget tools under the "memory" set. Must be called after the
// memory pipeline is created.
func RegisterMemoryTools(reg *CapabilityRegistry, pipeline *memory.Pipeline) {
	RegisterSetTools(reg, SetMemory, "Memory", tools.MemoryToolsList(pipeline)...)
}
