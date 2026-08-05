package agent

import (
	"context"
	"fmt"
	"strings"
)

// MemoryTreeAccessor is the interface for reading from the memory tree.
type MemoryTreeAccessor interface {
	// GetRootSummaries returns summaries of the top-level tree nodes.
	GetRootSummaries() []MemoryNodeSummary
	// Search finds nodes matching a query.
	Search(query string, limit int) ([]MemoryNodeSummary, error)
}

// MemoryNodeSummary is a lightweight view of a tree node for context injection.
type MemoryNodeSummary struct {
	ID      string
	Content string // first 500 chars
	Summary string
	Count   int
}

// MemoryPrefetcher fetches relevant context from the memory tree and
// injects it into the system prompt for cross-session awareness.
type MemoryPrefetcher struct {
	tree MemoryTreeAccessor
}

// NewMemoryPrefetcher creates a prefetcher backed by a tree accessor.
func NewMemoryPrefetcher(tree MemoryTreeAccessor) *MemoryPrefetcher {
	return &MemoryPrefetcher{tree: tree}
}

// Prefetch retrieves relevant memory context for the given message.
func (mp *MemoryPrefetcher) Prefetch(ctx context.Context, userMessage string) string {
	if mp.tree == nil {
		return ""
	}

	// Get root summaries for broad context.
	roots := mp.tree.GetRootSummaries()
	if len(roots) == 0 && userMessage == "" {
		return ""
	}

	var b strings.Builder

	// Root summaries: always include a digest of what the user has been working on.
	if len(roots) > 0 {
		b.WriteString("## Recent Memory Context\n\n")
		b.WriteString("The following is a summary of your user's recent activity across sessions:\n\n")
		for _, r := range roots {
			if r.Summary != "" {
				b.WriteString(fmt.Sprintf("- **%s**: %s\n", r.ID, truncateStr(r.Summary, 300)))
			} else if r.Content != "" {
				b.WriteString(fmt.Sprintf("- %s: %s\n", r.ID, truncateStr(r.Content, 200)))
			}
		}
		if totalItems := totalCount(roots); totalItems > 0 {
			b.WriteString(fmt.Sprintf("\n(%d total memory items across %d topics)\n", totalItems, len(roots)))
		}
		b.WriteString("\n")
	}

	// Query-specific search: if the user message contains specific terms, search.
	if userMessage != "" {
		results, err := mp.tree.Search(userMessage, 5)
		if err == nil && len(results) > 0 {
			b.WriteString("## Relevant Past Context\n\n")
			for _, r := range results {
				text := r.Summary
				if text == "" {
					text = truncateStr(r.Content, 200)
				}
				b.WriteString(fmt.Sprintf("- %s\n", text))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// FormatForPrompt formats prefetched context for system prompt injection.
func (mp *MemoryPrefetcher) FormatForPrompt(ctx context.Context, systemPrompt, userMessage string) string {
	context := mp.Prefetch(ctx, userMessage)
	if context == "" {
		return systemPrompt
	}
	return systemPrompt + "\n\n" + context
}

func totalCount(roots []MemoryNodeSummary) int {
	total := 0
	for _, r := range roots {
		total += r.Count
	}
	return total
}

// ── Pipeline adapter ──────────────────────────────────────────────────────

// PipelineTreeAdapter adapts a memory pipeline to the MemoryTreeAccessor interface.
type PipelineTreeAdapter struct {
	getSummaries func() []MemoryNodeSummary
	searchFn     func(query string, limit int) ([]MemoryNodeSummary, error)
}

// NewPipelineTreeAdapter creates an adapter from a pipeline.
func NewPipelineTreeAdapter(
	getSummaries func() []MemoryNodeSummary,
	searchFn func(query string, limit int) ([]MemoryNodeSummary, error),
) *PipelineTreeAdapter {
	return &PipelineTreeAdapter{
		getSummaries: getSummaries,
		searchFn:     searchFn,
	}
}

func (a *PipelineTreeAdapter) GetRootSummaries() []MemoryNodeSummary {
	if a.getSummaries == nil {
		return nil
	}
	return a.getSummaries()
}

func (a *PipelineTreeAdapter) Search(query string, limit int) ([]MemoryNodeSummary, error) {
	if a.searchFn == nil {
		return nil, nil
	}
	return a.searchFn(query, limit)
}
