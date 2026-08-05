package capability

import (
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/tools"
)

// RegisterMemoryTools registers memory_search, memory_save, memory_recall,
// and memory_forget tools. Must be called after the memory pipeline is created.
func RegisterMemoryTools(reg *CapabilityRegistry, pipeline *memory.Pipeline) {
	for _, t := range tools.MemoryToolsList(pipeline) {
		reg.RegisterTool("builtin", t)
	}
}
