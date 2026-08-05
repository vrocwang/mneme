package middleware

import (
	"context"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/simon/mneme/internal/agent"
	ctxmgr "github.com/simon/mneme/internal/context"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/memory/profile"
)

// MemoryMiddleware bridges the retained Memory Pipeline into eino,
// injecting memory context before each agent call and triggering
// extraction after each turn.
//
// All fields are safely nil-checked: when a dependency is nil its
// corresponding injection/extraction step is silently skipped.
type MemoryMiddleware struct {
	Pipeline   *memory.Pipeline
	Prefetcher *agent.MemoryPrefetcher
	Profile    *profile.Store
	Tracker    *ctxmgr.SessionMemoryTracker
	ToolRules  *memory.ToolRuleStore
	Log        *slog.Logger

	// Optional injectors that were previously wired via the context Manager
	// enrichers. Each injects relevant context into the system prompt or
	// user message before every agent call.
	Skills    *agent.SkillsInjector
	Exp       *agent.ExperienceInjector
	Workflows *agent.WorkflowInjector
}

// ModifyMessages injects memory context into the system message before
// each agent invocation. It:
//  1. Uses the MemoryPrefetcher to add recent memory tree context
//  2. Adds user profile facets from the profile store
//  3. Adds tool-specific rules from the tool rule store
//
// When no system message exists in the slice, one is prepended.
// Returns the (potentially modified) message slice.
func (m *MemoryMiddleware) ModifyMessages(ctx context.Context, msgs []*schema.Message) []*schema.Message {
	if m == nil {
		return msgs
	}

	// Find the last system message or create one.
	sysIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] != nil && msgs[i].Role == schema.System {
			sysIdx = i
			break
		}
	}

	var sysMsg *schema.Message
	if sysIdx >= 0 {
		sysMsg = msgs[sysIdx]
	} else {
		sysMsg = schema.SystemMessage("")
		msgs = append([]*schema.Message{sysMsg}, msgs...)
		sysIdx = 0
	}

	// Extract the last user message for prefetch targeting.
	userMessage := m.lastUserContent(msgs)

	// 1. Memory prefetch context — recent memory tree summaries and
	//    query-specific search results.
	if m.Prefetcher != nil {
		pctx := m.Prefetcher.Prefetch(ctx, userMessage)
		if pctx != "" {
			sysMsg.Content = m.appendSection(sysMsg.Content, pctx)
		}
	}

	// 2. Profile facets — active user preferences, roles, skills, and
	//    personality traits accumulated across sessions.
	if m.Profile != nil {
		facets, err := m.Profile.GetActiveFacets(20)
		if err == nil && len(facets) > 0 {
			if section := profile.RenderProfileContext(facets); section != "" {
				sysMsg.Content = m.appendSection(sysMsg.Content, section)
			}
		}
	}

	// 3. Tool rules — critical/high-priority tool-specific rules that
	//    the agent should follow when invoking certain tools.
	if m.ToolRules != nil {
		if section := m.ToolRules.BuildPromptSection(); section != "" {
			sysMsg.Content = m.appendSection(sysMsg.Content, section)
		}
	}

	// 4. Skills — installable SKILL.md files that match the user's request.
	if m.Skills != nil {
		sysMsg.Content = m.Skills.InjectPrompt(ctx, userMessage, sysMsg.Content)
	}

	// 5. Workflows — reusable phase-keyed workflow guidance.
	if m.Workflows != nil {
		sysMsg.Content = m.Workflows.InjectPrompt(ctx, userMessage, sysMsg.Content)
	}

	// 6. Past experiences — prior learnings prepended to the user message.
	if m.Exp != nil {
		exps, err := m.Exp.GetRelevant(ctx, userMessage, 5)
		if err == nil && len(exps) > 0 {
			for i, msg := range msgs {
				if msg != nil && msg.Role == schema.User {
					msgs[i].Content = agent.FormatForPrompt(exps, msg.Content)
					break
				}
			}
		}
	}

	return msgs
}

// OnTurnEnd records the turn's metrics and, when the session tracker
// indicates enough content has accumulated, triggers a background memory
// extraction via the pipeline's ArchiveConversation.
//
// Extraction is run in a background goroutine so it never blocks the
// agent loop. The tracker prevents concurrent extractions via
// MarkExtracting / MarkDone / MarkFailed.
func (m *MemoryMiddleware) OnTurnEnd(ctx context.Context, threadID string, tokenDelta int, toolCalls int) {
	if m == nil {
		return
	}

	// Record the turn metrics.
	if m.Tracker != nil {
		m.Tracker.NoteTurn(tokenDelta, toolCalls)
	}

	// Check if enough content has accumulated to warrant extraction.
	if m.Tracker != nil && !m.Tracker.ShouldExtract() {
		return
	}

	// Acquire the extraction lock.
	if m.Tracker != nil && !m.Tracker.MarkExtracting() {
		return // another extraction is already in progress
	}

	// Trigger archive in background with detached context and panic recovery.
	if m.Pipeline != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					if m.Log != nil {
						m.Log.Error("memory extraction panicked", "thread_id", threadID, "panic", r)
					}
					if m.Tracker != nil {
						m.Tracker.MarkFailed()
					}
				}
			}()
			// Use a detached context so the archive completes even if the
			// parent request context is cancelled.
			if err := m.Pipeline.ArchiveConversation(threadID); err != nil {
				if m.Log != nil {
					m.Log.Warn("memory extraction failed", "thread_id", threadID, "error", err)
				}
				if m.Tracker != nil {
					m.Tracker.MarkFailed()
				}
				return
			}
			if m.Log != nil {
				m.Log.Debug("memory extraction complete", "thread_id", threadID)
			}
			if m.Tracker != nil {
				m.Tracker.MarkDone()
			}
		}()
	}
}

// lastUserContent returns the content of the last user message in the
// slice, scanning from the end backwards.
func (m *MemoryMiddleware) lastUserContent(msgs []*schema.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] != nil && msgs[i].Role == schema.User {
			return msgs[i].Content
		}
	}
	return ""
}

// appendSection appends a section to existing content with appropriate
// newline separators.
func (m *MemoryMiddleware) appendSection(existing, section string) string {
	if existing == "" {
		return section
	}
	if strings.HasSuffix(existing, "\n") {
		return existing + "\n" + section
	}
	return existing + "\n\n" + section
}
