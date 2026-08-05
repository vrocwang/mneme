package threads

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

// RegisterTools registers all thread-management tools into the given capability registry.
func RegisterTools(capReg *capability.CapabilityRegistry, ops *Ops) {
	capReg.RegisterTool("builtin", &threadListTool{ops})
	capReg.RegisterTool("builtin", &threadReadTool{ops})
	capReg.RegisterTool("builtin", &threadCreateTool{ops})
	capReg.RegisterTool("builtin", &threadMessageAppendTool{ops})
	capReg.RegisterTool("builtin", &threadUpdateTitleTool{ops})
	capReg.RegisterTool("builtin", &threadDeleteTool{ops})
}

// ── thread_list ───────────────────────────────────────────────────────

type threadListTool struct{ ops *Ops }

func (t *threadListTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "thread_list",
		Description: "Lists recent conversation threads, newest first. Returns thread IDs, titles, message counts, and timestamps.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit": map[string]interface{}{"type": "integer", "description": "Max threads to return (default: 20, max: 100)."},
			},
		},
	}
}

func (t *threadListTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	limit := 20
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	if limit > 100 {
		limit = 100
	}
	threads, err := t.ops.List(limit)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(threads)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── thread_read ────────────────────────────────────────────────────────

type threadReadTool struct{ ops *Ops }

func (t *threadReadTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "thread_read",
		Description: "Reads messages from a specific conversation thread. Returns messages in chronological order.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID to read."},
				"limit":     map[string]interface{}{"type": "integer", "description": "Max messages to return (default: 50)."},
			},
			"required": []string{"thread_id"},
		},
	}
}

func (t *threadReadTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	if threadID == "" {
		return tools.Result{Error: "thread_id is required"}
	}
	limit := 50
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	msgs, err := t.ops.ListMessages(threadID, limit, 0)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(msgs)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── thread_create ──────────────────────────────────────────────────────

type threadCreateTool struct{ ops *Ops }

func (t *threadCreateTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "thread_create",
		Description: "Creates a new conversation thread with an optional title. Returns the new thread ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title":  map[string]interface{}{"type": "string", "description": "Optional title for the new thread."},
				"labels": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional labels."},
			},
		},
	}
}

func (t *threadCreateTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	title, _ := args["title"].(string)
	var labels []string
	if labelsRaw, ok := args["labels"].([]interface{}); ok {
		for _, l := range labelsRaw {
			if s, ok := l.(string); ok {
				labels = append(labels, s)
			}
		}
	}
	thread, err := t.ops.CreateNew(title, labels, "")
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(thread)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── thread_message_append ──────────────────────────────────────────────

type threadMessageAppendTool struct{ ops *Ops }

func (t *threadMessageAppendTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "thread_message_append",
		Description: "Appends a message to a conversation thread. Use to save notes, summaries, or context for later retrieval.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID to append to."},
				"content":   map[string]interface{}{"type": "string", "description": "Message content."},
				"role":      map[string]interface{}{"type": "string", "description": "Message role: user, assistant, or system (default: system)."},
			},
			"required": []string{"thread_id", "content"},
		},
	}
}

func (t *threadMessageAppendTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	content, _ := args["content"].(string)
	if threadID == "" || content == "" {
		return tools.Result{Error: "thread_id and content are required"}
	}
	role, _ := args["role"].(string)
	if role == "" {
		role = "system"
	}
	msg, err := t.ops.AppendMessage(threadID, role, content, nil)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── thread_update_title ────────────────────────────────────────────────

type threadUpdateTitleTool struct{ ops *Ops }

func (t *threadUpdateTitleTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "thread_update_title",
		Description: "Updates the title of a conversation thread.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID to update."},
				"title":     map[string]interface{}{"type": "string", "description": "New title."},
			},
			"required": []string{"thread_id", "title"},
		},
	}
}

func (t *threadUpdateTitleTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	title, _ := args["title"].(string)
	if threadID == "" || title == "" {
		return tools.Result{Error: "thread_id and title are required"}
	}
	if err := t.ops.UpdateTitle(threadID, title); err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Thread %q renamed to %q", threadID, title)}
}

// ── thread_delete ──────────────────────────────────────────────────────

type threadDeleteTool struct{ ops *Ops }

func (t *threadDeleteTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "thread_delete",
		Description: "Deletes a conversation thread and all its messages. This action cannot be undone.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thread_id": map[string]interface{}{"type": "string", "description": "The thread ID to delete."},
			},
			"required": []string{"thread_id"},
		},
	}
}

func (t *threadDeleteTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	threadID, _ := args["thread_id"].(string)
	if threadID == "" {
		return tools.Result{Error: "thread_id is required"}
	}
	if err := t.ops.Delete(threadID); err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Thread %q deleted", threadID)}
}
