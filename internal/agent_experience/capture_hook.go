package agent_experience

import (
	"context"
	"strings"

	"github.com/simon/mneme/internal/agent"
)

// CaptureHook is a TurnEndHook that automatically extracts and persists
// agent experiences after each turn. Wire it into the agent loop via
// loop.Hooks.AddTurnEnd(captureHook.OnTurnEnd).
type CaptureHook struct {
	store *Store
}

// NewCaptureHook creates a turn-end hook that captures experiences.
func NewCaptureHook(store *Store) *CaptureHook {
	return &CaptureHook{store: store}
}

// OnTurnEnd implements agent.TurnEndHook.
func (h *CaptureHook) OnTurnEnd(ctx context.Context, hc agent.HookContext, result *agent.TurnResult) {
	if h.store == nil || result == nil {
		return
	}

	// Build turn context from the available hook data.
	tc := TurnContext{
		AgentID:        hc.AgentID,
		SessionID:      hc.ThreadID,
		IterationCount: result.Rounds,
		TurnDurationMs: result.TotalDuration.Milliseconds(),
	}

	// Derive the user message from the first tool call or the response.
	if len(result.ToolCalls) > 0 {
		first := result.ToolCalls[0]
		if len(first.Args) > 0 {
			if task, ok := first.Args["task"].(string); ok && task != "" {
				tc.UserMessage = task
			}
		}
	}
	if tc.UserMessage == "" {
		tc.UserMessage = truncateStr(result.Response, 280)
	}

	// Convert TurnResult.ToolCalls to TurnContext.ToolCalls.
	for _, tcr := range result.ToolCalls {
		record := ToolCallRecord{
			Name:       tcr.Name,
			Success:    tcr.Error == "",
			DurationMs: tcr.Duration.Milliseconds(),
		}
		if tcr.Error != "" {
			record.OutputSummary = tcr.Error
		} else {
			record.OutputSummary = truncateStr(tcr.Output, 200)
		}
		if tcr.Args != nil {
			record.Arguments = tcr.Args
		}
		tc.ToolCalls = append(tc.ToolCalls, record)
	}

	if len(tc.ToolCalls) == 0 {
		return
	}

	// Extract and persist candidates.
	candidates := ExtractCandidates(tc)
	for _, c := range candidates {
		h.store.Put(c)
	}
}

func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max]))
}
