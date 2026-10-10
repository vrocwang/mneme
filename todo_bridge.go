package main

// Todo board RPC bridge.
//
// The task-board store and its tools live in the `todos` extension (see
// extensions/todos). The App RPC methods the frontend already calls are kept as
// a thin bridge: they invoke the extension's tools through the capability
// registry and hand back the board JSON unchanged. That keeps one
// implementation of the board (the extension owns the file format and the
// locking) while the UI keeps its existing API.

import (
	"context"
	"encoding/json"
	"fmt"
)

// todoCard mirrors the extension's card JSON.
type todoCard struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Order      int    `json:"order"`
	Notes      string `json:"notes,omitempty"`
	AssignedTo string `json:"assignedTo,omitempty"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// todoSnapshot mirrors the extension's board + rendered markdown.
type todoSnapshot struct {
	ThreadID string     `json:"threadId"`
	Cards    []todoCard `json:"cards"`
	Markdown string     `json:"markdown"`
}

// callTodoTool runs one of the todos extension's tools and decodes its board
// output. A tool-level failure (unknown thread, missing card) is returned as an
// error so the UI can surface it.
func (a *App) callTodoTool(name string, args map[string]interface{}) (*todoSnapshot, error) {
	if a.CapReg == nil {
		return nil, fmt.Errorf("capability registry not available")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	res := a.CapReg.Execute(ctx, name, args)
	if res.Error != "" {
		return nil, fmt.Errorf("%s: %s", name, res.Error)
	}
	var snap todoSnapshot
	if err := json.Unmarshal([]byte(res.Output), &snap); err != nil {
		return nil, fmt.Errorf("%s returned an unreadable board: %w", name, err)
	}
	return &snap, nil
}

// ListTodos returns the board for a thread.
func (a *App) ListTodos(threadID string) (*todoSnapshot, error) {
	return a.callTodoTool("todo_list", map[string]interface{}{"thread_id": threadID})
}

// AddTodo adds a card and returns the updated board.
func (a *App) AddTodo(threadID, title, notes string) (*todoSnapshot, error) {
	return a.callTodoTool("todo_add", map[string]interface{}{
		"thread_id": threadID, "title": title, "notes": notes,
	})
}

// UpdateTodoStatus moves a card to a new status and returns the updated board.
func (a *App) UpdateTodoStatus(threadID, cardID, status string) (*todoSnapshot, error) {
	return a.callTodoTool("todo_update_status", map[string]interface{}{
		"thread_id": threadID, "card_id": cardID, "status": status,
	})
}

// RemoveTodo deletes a card and returns the updated board.
func (a *App) RemoveTodo(threadID, cardID string) (*todoSnapshot, error) {
	return a.callTodoTool("todo_remove", map[string]interface{}{
		"thread_id": threadID, "card_id": cardID,
	})
}
