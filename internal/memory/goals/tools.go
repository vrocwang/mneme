package goals

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

// RegisterTools registers all goal management tools into the capability registry
// under the "goals" set.
func RegisterTools(reg *capability.CapabilityRegistry, store *Store) {
	if reg == nil || store == nil {
		return
	}
	reg.EnsureSet(&capability.CapabilitySet{
		ID:      "goals",
		Name:    "Goals",
		Kind:    capability.KindBuiltin,
		Enabled: true,
	})
	reg.RegisterTool("goals", &listTool{store: store})
	reg.RegisterTool("goals", &addTool{store: store})
	reg.RegisterTool("goals", &editTool{store: store})
	reg.RegisterTool("goals", &deleteTool{store: store})
}

// ── goals_list ───────────────────────────────────────────────────────────

type listTool struct {
	store *Store
}

func (t *listTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "goals_list",
		Description: "List all persistent long-term goals. Goals are durable objectives the agent tracks across sessions.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *listTool) Execute(_ context.Context, _ map[string]interface{}) tools.Result {
	doc, err := t.store.Load()
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Failed to load goals: %v", err)}
	}
	if len(doc.Items) == 0 {
		return tools.Result{Success: true, Output: "No goals set. Use goals_add to create one."}
	}
	var b strings.Builder
	b.WriteString("Current goals:\n")
	for _, item := range doc.Items {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", item.ID, item.Text))
	}
	return tools.Result{Success: true, Output: b.String()}
}

func (t *listTool) ConcurrencySafe() bool                  { return true }
func (t *listTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }

// ── goals_add ────────────────────────────────────────────────────────────

type addTool struct {
	store *Store
}

func (t *addTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "goals_add",
		Description: "Add a new long-term goal. Returns the assigned goal ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "The goal text (one concise sentence).",
				},
			},
			"required": []string{"text"},
		},
	}
}

func (t *addTool) Execute(_ context.Context, args map[string]interface{}) tools.Result {
	text, _ := args["text"].(string)
	if text == "" {
		return tools.Result{Error: "text is required"}
	}
	item, err := t.store.Add(text)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Failed to add goal: %v", err)}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Goal added: [%s] %s", item.ID, item.Text)}
}

func (t *addTool) PermissionLevel() tools.PermissionLevel { return tools.PermWrite }
func (t *addTool) ConcurrencySafe() bool                  { return false }

// ── goals_edit ───────────────────────────────────────────────────────────

type editTool struct {
	store *Store
}

func (t *editTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "goals_edit",
		Description: "Edit an existing long-term goal by its ID (e.g. g1).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The goal ID from goals_list (e.g. g1).",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "The updated goal text.",
				},
			},
			"required": []string{"id", "text"},
		},
	}
}

func (t *editTool) Execute(_ context.Context, args map[string]interface{}) tools.Result {
	id, _ := args["id"].(string)
	text, _ := args["text"].(string)
	if id == "" || text == "" {
		return tools.Result{Error: "id and text are required"}
	}
	item, err := t.store.Edit(id, text)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Failed to edit goal: %v", err)}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Goal updated: [%s] %s", item.ID, item.Text)}
}

func (t *editTool) PermissionLevel() tools.PermissionLevel { return tools.PermWrite }
func (t *editTool) ConcurrencySafe() bool                  { return false }

// ── goals_delete ─────────────────────────────────────────────────────────

type deleteTool struct {
	store *Store
}

func (t *deleteTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "goals_delete",
		Description: "Delete a long-term goal by its ID (e.g. g1).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The goal ID to delete (e.g. g1).",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t *deleteTool) Execute(_ context.Context, args map[string]interface{}) tools.Result {
	id, _ := args["id"].(string)
	if id == "" {
		return tools.Result{Error: "id is required"}
	}
	if err := t.store.Delete(id); err != nil {
		return tools.Result{Error: fmt.Sprintf("Failed to delete goal: %v", err)}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Goal %q deleted.", id)}
}

func (t *deleteTool) PermissionLevel() tools.PermissionLevel { return tools.PermWrite }
func (t *deleteTool) ConcurrencySafe() bool                  { return false }
