// Todos extension for Mneme.
//
// Provides the workspace-file task board tools:
//   - todo_list, todo_add, todo_edit, todo_update_status, todo_remove, todo_clear
//
// This was previously compiled into the core as internal/todos. The task
// manager agent prompt (internal/prompts/defaults/agent_task_manager.txt)
// references todo_list by name, so the tool names are part of the contract and
// must not change. Boards persist to <workspace>/agent_task_boards/<thread>.json,
// exactly where the core store wrote them; the workspace comes from MNEME_HOME,
// which the host exports before launching extensions.
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "todos",
		Version:     "0.1.0",
		Description: "Workspace-file task board (kanban) for agent task tracking",
	})

	t := &todoTools{store: NewStore()}

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "todo_list",
		Description: "Lists all task cards on the board for a given thread.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID whose task board to list."},
			},
			"required": []string{"thread_id"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, t.list)

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "write",
		HasEffects: true,
	}, t.add)

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "write",
		HasEffects: true,
	}, t.edit)

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "write",
		HasEffects: true,
	}, t.updateStatus)

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "write",
		HasEffects: true,
	}, t.remove)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "todo_clear",
		Description: "Clears all task cards from a thread's task board.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID."},
			},
			"required": []string{"thread_id"},
		},
		Permission: "write",
		HasEffects: true,
	}, t.clear)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "todos: %v\n", err)
		os.Exit(1)
	}
}

// todoTools holds the board store shared by every tool handler.
type todoTools struct{ store *Store }

// ── todo_list ──────────────────────────────────────────────────────────

func (t *todoTools) list(_ context.Context, args map[string]interface{}) extsdk.Result {
	threadID := extsdk.GetStr(args, "thread_id", "")
	if threadID == "" {
		return extsdk.Result{Error: "thread_id is required"}
	}
	snap, err := t.store.List(threadID)
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

// ── todo_add ──────────────────────────────────────────────────────────

func (t *todoTools) add(_ context.Context, args map[string]interface{}) extsdk.Result {
	threadID := extsdk.GetStr(args, "thread_id", "")
	title := extsdk.GetStr(args, "title", "")
	notes := extsdk.GetStr(args, "notes", "")
	if threadID == "" || title == "" {
		return extsdk.Result{Error: "thread_id and title are required"}
	}
	snap, err := t.store.Add(threadID, title, notes)
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

// ── todo_edit ──────────────────────────────────────────────────────────

func (t *todoTools) edit(_ context.Context, args map[string]interface{}) extsdk.Result {
	threadID := extsdk.GetStr(args, "thread_id", "")
	cardID := extsdk.GetStr(args, "card_id", "")
	title := extsdk.GetStr(args, "title", "")
	notes := extsdk.GetStr(args, "notes", "")
	if threadID == "" || cardID == "" {
		return extsdk.Result{Error: "thread_id and card_id are required"}
	}
	snap, err := t.store.Edit(threadID, cardID, title, notes)
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

// ── todo_update_status ─────────────────────────────────────────────────

func (t *todoTools) updateStatus(_ context.Context, args map[string]interface{}) extsdk.Result {
	threadID := extsdk.GetStr(args, "thread_id", "")
	cardID := extsdk.GetStr(args, "card_id", "")
	status := extsdk.GetStr(args, "status", "")
	if threadID == "" || cardID == "" || status == "" {
		return extsdk.Result{Error: "thread_id, card_id, and status are required"}
	}
	snap, err := t.store.UpdateStatus(threadID, cardID, Status(status))
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

// ── todo_remove ────────────────────────────────────────────────────────

func (t *todoTools) remove(_ context.Context, args map[string]interface{}) extsdk.Result {
	threadID := extsdk.GetStr(args, "thread_id", "")
	cardID := extsdk.GetStr(args, "card_id", "")
	if threadID == "" || cardID == "" {
		return extsdk.Result{Error: "thread_id and card_id are required"}
	}
	snap, err := t.store.Remove(threadID, cardID)
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

// ── todo_clear ─────────────────────────────────────────────────────────

func (t *todoTools) clear(_ context.Context, args map[string]interface{}) extsdk.Result {
	threadID := extsdk.GetStr(args, "thread_id", "")
	if threadID == "" {
		return extsdk.Result{Error: "thread_id is required"}
	}
	if err := t.store.Clear(threadID); err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Cleared task board for thread %q", threadID)}
}
