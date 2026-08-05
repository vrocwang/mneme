package diff

import (
	"context"
	"time"

	"github.com/simon/mneme/internal/tools"
)

// MemoryChangesTool lets the agent query recent memory changes.
// Registered via capability.RegisterMemoryDiffTools.
type MemoryChangesTool struct {
	store *Store
}

// NewMemoryChangesTool creates an agent tool for memory change queries.
func NewMemoryChangesTool(store *Store) tools.Tool {
	return &MemoryChangesTool{store: store}
}

func (t *MemoryChangesTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "memory_changes",
		Description: "List recent memory changes (added, updated, or removed items) from external sync sources like GitHub, RSS, Gmail, etc. Use this to discover what's new since you last checked.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"since": map[string]interface{}{
					"type":        "string",
					"description": "Time window: '1h', '6h', '24h', '7d' (default: '24h')",
				},
			},
		},
	}
}

func (t *MemoryChangesTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	sinceStr, _ := args["since"].(string)
	since := 24 * time.Hour
	switch sinceStr {
	case "1h":
		since = time.Hour
	case "6h":
		since = 6 * time.Hour
	case "24h":
		since = 24 * time.Hour
	case "7d":
		since = 7 * 24 * time.Hour
	}

	events, err := t.store.Recent(ctx, since, 20)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: FormatMarkdown(events)}
}

func (t *MemoryChangesTool) ConcurrencySafe() bool                  { return true }
func (t *MemoryChangesTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }
