package todos

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

// RegisterTools registers all todo/task-board tools into the given capability registry
// under the "todos" set.
func RegisterTools(capReg *capability.CapabilityRegistry, store *Store) {
	capReg.EnsureSet(&capability.CapabilitySet{
		ID:      "todos",
		Name:    "Todos",
		Kind:    capability.KindBuiltin,
		Enabled: true,
	})
	capReg.RegisterTool("todos", &todoListTool{store})
	capReg.RegisterTool("todos", &todoAddTool{store})
	capReg.RegisterTool("todos", &todoEditTool{store})
	capReg.RegisterTool("todos", &todoUpdateStatusTool{store})
	capReg.RegisterTool("todos", &todoRemoveTool{store})
	capReg.RegisterTool("todos", &todoClearTool{store})
}

// ── todo_list ──────────────────────────────────────────────────────────

type todoListTool struct{ store *Store }

func (t *todoListTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "todo_list",
		Description: "Lists all task cards on the board for a given thread.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID whose task board to list."},
			},
			"required": []string{"thread_id"},
		},
	}
}

func (t *todoListTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	if threadID == "" {
		return tools.Result{Error: "thread_id is required"}
	}
	snap, err := t.store.List(threadID)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── todo_add ──────────────────────────────────────────────────────────

type todoAddTool struct{ store *Store }

func (t *todoAddTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "todo_add",
		Description: "Adds a new task card to a thread's task board.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID."},
				"title":     map[string]interface{}{"type": "string", "description": "Task title."},
				"notes":     map[string]interface{}{"type": "string", "description": "Optional notes/details."},
			},
			"required": []string{"thread_id", "title"},
		},
	}
}

func (t *todoAddTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	title, _ := args["title"].(string)
	notes, _ := args["notes"].(string)
	if threadID == "" || title == "" {
		return tools.Result{Error: "thread_id and title are required"}
	}
	snap, err := t.store.Add(threadID, title, notes)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── todo_edit ──────────────────────────────────────────────────────────

type todoEditTool struct{ store *Store }

func (t *todoEditTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "todo_edit",
		Description: "Edits a task card's title or notes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID."},
				"card_id":   map[string]interface{}{"type": "string", "description": "The card ID to edit."},
				"title":     map[string]interface{}{"type": "string", "description": "New title (optional)."},
				"notes":     map[string]interface{}{"type": "string", "description": "New notes (optional)."},
			},
			"required": []string{"thread_id", "card_id"},
		},
	}
}

func (t *todoEditTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	cardID, _ := args["card_id"].(string)
	title, _ := args["title"].(string)
	notes, _ := args["notes"].(string)
	if threadID == "" || cardID == "" {
		return tools.Result{Error: "thread_id and card_id are required"}
	}
	snap, err := t.store.Edit(threadID, cardID, title, notes)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── todo_update_status ─────────────────────────────────────────────────

type todoUpdateStatusTool struct{ store *Store }

func (t *todoUpdateStatusTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "todo_update_status",
		Description: "Updates a task card's status. Valid statuses: todo, in_progress, blocked, done.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID."},
				"card_id":   map[string]interface{}{"type": "string", "description": "The card ID to update."},
				"status":    map[string]interface{}{"type": "string", "description": "New status: todo, in_progress, blocked, or done."},
			},
			"required": []string{"thread_id", "card_id", "status"},
		},
	}
}

func (t *todoUpdateStatusTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	cardID, _ := args["card_id"].(string)
	status, _ := args["status"].(string)
	if threadID == "" || cardID == "" || status == "" {
		return tools.Result{Error: "thread_id, card_id, and status are required"}
	}
	snap, err := t.store.UpdateStatus(threadID, cardID, Status(status))
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── todo_remove ────────────────────────────────────────────────────────

type todoRemoveTool struct{ store *Store }

func (t *todoRemoveTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "todo_remove",
		Description: "Removes a task card from a thread's task board.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID."},
				"card_id":   map[string]interface{}{"type": "string", "description": "The card ID to remove."},
			},
			"required": []string{"thread_id", "card_id"},
		},
	}
}

func (t *todoRemoveTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	cardID, _ := args["card_id"].(string)
	if threadID == "" || cardID == "" {
		return tools.Result{Error: "thread_id and card_id are required"}
	}
	snap, err := t.store.Remove(threadID, cardID)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── todo_clear ────────────────────────────────────────────────────────

type todoClearTool struct{ store *Store }

func (t *todoClearTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "todo_clear",
		Description: "Clears all task cards from a thread's task board.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID."},
			},
			"required": []string{"thread_id"},
		},
	}
}

func (t *todoClearTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	if threadID == "" {
		return tools.Result{Error: "thread_id is required"}
	}
	if err := t.store.Clear(threadID); err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Cleared task board for thread %q", threadID)}
}
