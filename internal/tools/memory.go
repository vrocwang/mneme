package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon/mneme/internal/memory"
)

// MemorySearchTool allows the agent to search memory.
type MemorySearchTool struct {
	pipeline *memory.Pipeline
}

func NewMemorySearchTool(p *memory.Pipeline) *MemorySearchTool {
	return &MemorySearchTool{pipeline: p}
}

func (t *MemorySearchTool) Schema() Schema {
	return Schema{
		Name:        "memory_search",
		Description: "Search your memory for information about a topic, person, or past conversation",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *MemorySearchTool) PermissionLevel() PermissionLevel { return PermReadOnly }
func (t *MemorySearchTool) SideEffects() bool                { return false }

func (t *MemorySearchTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	query, _ := args["query"].(string)
	if query == "" {
		return Result{Error: "query is required"}
	}
	if t.pipeline == nil {
		return Result{Error: "memory pipeline not available"}
	}

	result, err := t.pipeline.Search(ctx, query, 10)
	if err != nil {
		return Result{Error: fmt.Sprintf("memory search: %v", err)}
	}

	return Result{Success: true, Output: result.Formatted()}
}

// MemorySaveTool saves a fact to memory.
type MemorySaveTool struct {
	pipeline *memory.Pipeline
}

func NewMemorySaveTool(p *memory.Pipeline) *MemorySaveTool {
	return &MemorySaveTool{pipeline: p}
}

func (t *MemorySaveTool) Schema() Schema {
	return Schema{
		Name:        "memory_save",
		Description: "Save an important fact or piece of information to your memory",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to remember",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Category: fact, preference, rule, or note",
				},
			},
			"required": []string{"content"},
		},
	}
}

func (t *MemorySaveTool) PermissionLevel() PermissionLevel { return PermWrite }
func (t *MemorySaveTool) SideEffects() bool                { return true }

func (t *MemorySaveTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	content, _ := args["content"].(string)
	category, _ := args["category"].(string)
	if content == "" {
		return Result{Error: "content is required"}
	}
	if category == "" {
		category = "note"
	}
	if t.pipeline == nil {
		return Result{Error: "memory pipeline not available"}
	}

	if err := t.pipeline.IndexContent("agent_"+category, content); err != nil {
		return Result{Error: fmt.Sprintf("memory save: %v", err)}
	}
	return Result{Success: true, Output: fmt.Sprintf("Saved to memory [%s]: %s", category, truncateStr(content, 100))}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// MemoryContextQuery searches memory and returns relevant snippets for prompt injection.
func MemoryContextQuery(p *memory.Pipeline, query string) string {
	if p == nil || query == "" {
		return ""
	}
	result, err := p.Search(context.Background(), query, 5)
	if err != nil || result.TotalResults() == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Relevant memories:\n")
	for _, c := range result.Chunks {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", c.Source, truncateStr(c.Content, 300)))
	}
	return b.String()
}

// ── Advanced memory tools ──────────────────────────────────────────────

// MemoryRecallTool performs semantic search via embeddings + FTS5 hybrid.
type MemoryRecallTool struct {
	pipeline *memory.Pipeline
}

func NewMemoryRecallTool(p *memory.Pipeline) *MemoryRecallTool {
	return &MemoryRecallTool{pipeline: p}
}

func (t *MemoryRecallTool) Schema() Schema {
	return Schema{
		Name:        "memory_recall",
		Description: "Performs deep semantic recall across all memory — FTS5 text, vector similarity, and knowledge graph. Use for complex or nuanced memory queries.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query for semantic recall.",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Max results (default: 10, max: 25).",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *MemoryRecallTool) PermissionLevel() PermissionLevel { return PermReadOnly }
func (t *MemoryRecallTool) SideEffects() bool                { return false }

func (t *MemoryRecallTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	query, _ := args["query"].(string)
	if query == "" {
		return Result{Error: "query is required"}
	}
	limit := 10
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
		if limit > 25 {
			limit = 25
		}
	}
	if t.pipeline == nil {
		return Result{Error: "memory pipeline not available"}
	}
	result, err := t.pipeline.Search(ctx, query, limit)
	if err != nil {
		return Result{Error: fmt.Sprintf("memory recall: %v", err)}
	}
	return Result{Success: true, Output: result.Formatted()}
}

// MemoryForgetTool removes specific memories by source or ID.
type MemoryForgetTool struct {
	pipeline *memory.Pipeline
}

func NewMemoryForgetTool(p *memory.Pipeline) *MemoryForgetTool {
	return &MemoryForgetTool{pipeline: p}
}

func (t *MemoryForgetTool) Schema() Schema {
	return Schema{
		Name:        "memory_forget",
		Description: "Forgets/removes a specific memory. Use sparingly and only when explicitly asked.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query to find the memory to forget.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Why this memory should be forgotten.",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *MemoryForgetTool) PermissionLevel() PermissionLevel { return PermWrite }
func (t *MemoryForgetTool) SideEffects() bool                { return true }

func (t *MemoryForgetTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	query, _ := args["query"].(string)
	reason, _ := args["reason"].(string)
	if query == "" {
		return Result{Error: "query is required"}
	}
	// Delete matching memory chunks from the store.
	if t.pipeline == nil {
		return Result{Error: "memory pipeline not available"}
	}
	n, err := t.pipeline.ForgetContent(query)
	if err != nil {
		return Result{Error: fmt.Sprintf("memory forget: %v", err)}
	}
	msg := fmt.Sprintf("Deleted %d memory chunks matching: %s", n, truncateStr(query, 100))
	if reason != "" {
		msg += fmt.Sprintf(" (reason: %s)", reason)
	}
	return Result{Success: true, Output: msg}
}
