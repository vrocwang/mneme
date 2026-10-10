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

// ── JSON-RPC HTTP surface (internal/jsonrpc.AppMethods) ─────────────
//
// These two keep the names jsonrpc.AppMethods requires. They intentionally
// mirror agent.AgentRPC.ListAgents / memory.MemoryRPC.ListAgents-style methods
// that the Wails frontend uses; the HTTP surface serves non-UI clients.

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

func (a *App) StreamChatMessage(req ChatRequest) {
	if a.ChatService == nil {
		runtime.EventsEmit(a.ctx, "chat:error", map[string]interface{}{"threadId": req.ThreadID, "error": "chat service not available"})
		return
	}
	a.ChatService.StreamMessage(a.ctx, req.ThreadID, req.Message, func(evt agent.StreamEvent) {
		runtime.EventsEmit(a.ctx, "chat:"+string(evt.Type), map[string]interface{}{
			"threadId": evt.ThreadID, "content": evt.Content, "args": evt.Args,
			"error": evt.Error, "done": evt.Done,
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
