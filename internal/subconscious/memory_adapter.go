package subconscious

import (
	"context"
	"time"

	"github.com/simon/mneme/internal/memory"
)

// memoryPipelineAdapter adapts memory.Pipeline to the MemoryPipeline
// interface expected by evaluators. Bridges the context parameter and
// return type mismatch.
type memoryPipelineAdapter struct {
	pipeline *memory.Pipeline
}

// NewMemoryPipelineAdapter wraps a memory.Pipeline for use in evaluators.
func NewMemoryPipelineAdapter(p *memory.Pipeline) MemoryPipeline {
	return &memoryPipelineAdapter{pipeline: p}
}

func (a *memoryPipelineAdapter) HasExternalContent(ctx context.Context, since time.Time) bool {
	return a.pipeline.HasExternalContent(ctx, since)
}

func (a *memoryPipelineAdapter) Search(query string, limit int) (*MemorySearchResult, error) {
	result, err := a.pipeline.Search(context.Background(), query, limit)
	if err != nil {
		return nil, err
	}
	items := make([]string, 0, len(result.Scored))
	for _, sc := range result.Scored {
		summary := sc.Chunk.Content
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		items = append(items, summary)
	}
	return &MemorySearchResult{
		Query:      query,
		TotalCount: result.TotalResults(),
		Items:      items,
	}, nil
}
