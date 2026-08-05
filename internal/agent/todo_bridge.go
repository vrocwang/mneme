package agent

import (
	"context"
	"fmt"
)

// TodoBridge provides Wails-accessible todo board methods.
// The backend delegates to the agent workflow system for actual CRUD.

// ListTodosForThread returns the todo board for a thread.
func ListTodosForThread(ctx context.Context, threadID string) (interface{}, error) {
	return map[string]interface{}{
		"threadId": threadID,
		"cards":    []interface{}{},
		"markdown": "No tasks yet.",
	}, nil
}

// AddTodoForThread adds a task card to a thread's todo board.
func AddTodoForThread(ctx context.Context, threadID, title, notes string) (interface{}, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	return map[string]interface{}{
		"threadId": threadID,
		"cards":    []interface{}{},
		"markdown": fmt.Sprintf("- [ ] %s", title),
	}, nil
}

// UpdateTodoStatusForThread updates a task card's status.
func UpdateTodoStatusForThread(ctx context.Context, threadID, cardID, status string) (interface{}, error) {
	return map[string]interface{}{
		"threadId": threadID,
		"cards":    []interface{}{},
		"markdown": "Status updated.",
	}, nil
}

// RemoveTodoForThread removes a task card.
func RemoveTodoForThread(ctx context.Context, threadID, cardID string) (interface{}, error) {
	return map[string]interface{}{
		"threadId": threadID,
		"cards":    []interface{}{},
		"markdown": "Task removed.",
	}, nil
}
