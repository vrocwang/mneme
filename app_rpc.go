package main

// This file contains RPC delegate methods on App that are exposed to the
// Wails frontend. They contain no business logic - each method delegates
// to the corresponding service or RPC object.
//
// Methods that have a dedicated RPC object (e.g. LearningRPC.GetPreferences)
// are kept here as thin wrappers for backward compatibility with frontend
// code that calls them via the App binding.

import (
	"context"
	"fmt"

	"github.com/simon/mneme/internal/agent"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ── jsonrpc.AppMethods implementation ──────────────────────────────

func (a *App) Health() map[string]interface{} { return a.AppCore.Health() }

func (a *App) ListAgents() []map[string]interface{} {
	if a.CapReg == nil {
		return nil
	}
	descs := a.CapReg.AllAgents()
	result := make([]map[string]interface{}, 0, len(descs))
	for _, d := range descs {
		if d.Hidden {
			continue
		}
		result = append(result, map[string]interface{}{
			"id": d.ID, "name": d.Name, "description": d.Description,
		})
	}
	return result
}

func (a *App) SearchMemory(query string) (string, error) {
	if a.Pipeline == nil {
		return "Memory pipeline not available.", nil
	}
	result, err := a.Pipeline.Search(context.Background(), query, a.Pipeline.DefaultSearchLimit())
	if err != nil {
		return "", err
	}
	return result.Formatted(), nil
}

// ── Approval RPC delegates ──────────────────────────────────────────

func (a *App) ListPendingApprovals() []map[string]interface{} {
	if a.ApprovalGate == nil {
		return nil
	}
	return a.ApprovalGate.ListPendingForUI()
}

func (a *App) DecideApproval(id string, approve bool) error {
	if a.ApprovalGate == nil {
		return fmt.Errorf("approval gate not initialized")
	}
	return a.ApprovalGate.DecideByBool(id, approve)
}

// ── Chat RPC delegates ──────────────────────────────────────────────

type ChatRequest struct {
	ThreadID string `json:"threadId"`
	Message  string `json:"message"`
}

type ChatResponse struct {
	ThreadID string `json:"threadId"`
	Content  string `json:"content"`
	Done     bool   `json:"done"`
}

func (a *App) StreamChatMessage(req ChatRequest) {
	if a.ChatService == nil {
		runtime.EventsEmit(a.ctx, "chat:error", map[string]interface{}{"threadId": req.ThreadID, "error": "chat service not available"})
		return
	}
	a.ChatService.StreamMessage(a.ctx, req.ThreadID, req.Message, func(evt agent.StreamEvent) {
		runtime.EventsEmit(a.ctx, "chat:"+evt.Type, map[string]interface{}{
			"threadId": evt.ThreadID, "content": evt.Content, "args": evt.Args, "done": evt.Done,
		})
	})
}

func (a *App) SendMessage(req ChatRequest) (ChatResponse, error) {
	if a.ChatService == nil {
		return ChatResponse{}, fmt.Errorf("chat service not available")
	}
	result, err := a.ChatService.SendMessage(a.ctx, req.ThreadID, req.Message)
	if err != nil {
		return ChatResponse{}, err
	}
	return ChatResponse{ThreadID: req.ThreadID, Content: result.Response, Done: true}, nil
}

func (a *App) CancelMessage(threadID string) {
	if a.ChatService != nil {
		a.ChatService.Cancel(threadID)
	}
}

// ── Other RPC delegates ─────────────────────────────────────────────

func (a *App) GetMetrics() map[string]interface{} {
	if a.Metrics == nil {
		return map[string]interface{}{}
	}
	snap := a.Metrics.Snapshot()
	return map[string]interface{}{
		"counters": snap.Counters, "gauges": snap.Gauges, "histograms": snap.Histograms,
	}
}

func (a *App) GetPreferences() []map[string]interface{} {
	if a.Learning == nil {
		return []map[string]interface{}{}
	}
	prefs := a.Learning.Preferences()
	result := make([]map[string]interface{}, len(prefs))
	for i, p := range prefs {
		result[i] = map[string]interface{}{"key": p.Key, "value": p.Value, "confidence": p.Confidence}
	}
	return result
}

func (a *App) ActivateCompanion() string {
	if a.gui == nil || a.gui.CompanionLoop == nil {
		return "Companion not available - no provider configured."
	}
	go a.gui.CompanionLoop.Activate(a.ctx)
	return "Companion activated"
}

func (a *App) GetCronJobs() []map[string]interface{} {
	if a.Cron == nil {
		return nil
	}
	jobs := a.Cron.List()
	result := make([]map[string]interface{}, len(jobs))
	for i, j := range jobs {
		result[i] = map[string]interface{}{"id": j.ID, "name": j.Name, "schedule": j.Schedule, "enabled": j.Enabled}
	}
	return result
}

// ── Todo bridge delegates ───────────────────────────────────────────

func (a *App) ListTodos(threadID string) (interface{}, error) {
	return agent.ListTodosForThread(a.ctx, threadID)
}
func (a *App) AddTodo(threadID, title, notes string) (interface{}, error) {
	return agent.AddTodoForThread(a.ctx, threadID, title, notes)
}
func (a *App) UpdateTodoStatus(threadID, cardID, status string) (interface{}, error) {
	return agent.UpdateTodoStatusForThread(a.ctx, threadID, cardID, status)
}
func (a *App) RemoveTodo(threadID, cardID string) (interface{}, error) {
	return agent.RemoveTodoForThread(a.ctx, threadID, cardID)
}

// ── Subconscious delegates ──────────────────────────────────────────

func (a *App) GetSubconsciousStats() map[string]interface{} {
	if a.Subcon == nil {
		return map[string]interface{}{}
	}
	return a.Subcon.GetStats()
}

func (a *App) GetReflections(limit int) []interface{} {
	if a.Subcon == nil {
		return []interface{}{}
	}
	refs := a.Subcon.GetReflections(limit)
	result := make([]interface{}, len(refs))
	for i := range refs {
		result[i] = refs[i]
	}
	return result
}
