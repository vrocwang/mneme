package tools

import (
	"github.com/simon/mneme/internal/memory"
)

// MemoryToolsList returns memory tools that depend on the pipeline.
func MemoryToolsList(p *memory.Pipeline) []Tool {
	return []Tool{
		NewMemorySearchTool(p),
		NewMemorySaveTool(p),
		NewMemoryRecallTool(p),
		NewMemoryForgetTool(p),
	}
}
