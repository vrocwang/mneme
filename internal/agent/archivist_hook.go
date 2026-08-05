// Package agent provides the ArchivistHook PostTurnHook that orchestrates
// background memory extraction after each agent turn. It connects conversation
// segments, event extraction, user profile accumulation, and memory tree
// ingestion — matching Rust's ArchivistHook (in agent/hooks.rs archivist module).
package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/simon/mneme/internal/embeddings"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/memory/events"
	"github.com/simon/mneme/internal/memory/profile"
	"github.com/simon/mneme/internal/memory/segments"
)

// conversationTurn is a single turn in a conversation, used for archiving
// cleaned conversation content into the memory tree.
type conversationTurn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// ArchivistHook is a PostTurnHook that manages conversation segmentation,
// event extraction, profile updates, and memory tree ingestion.
type ArchivistHook struct {
	db             *sql.DB
	enabled        bool
	segmentStore   *segments.Store
	eventStore     *events.Store
	profileStore   *profile.Store
	boundaryConfig segments.BoundaryConfig
	log            *slog.Logger

	// Optional: LLM provider for segment summarization and Tier B event extraction.
	chatProvider inference.Provider
	model        string

	// Optional: embedding provider for segment embeddings.
	embedder embeddings.Provider

	// Memory pipeline for tree ingestion.
	pipeline MemoryPipelineIngestor

	// Turn buffer for conversation-to-tree archival.
	// Keyed by session ID; flushed when a segment is closed.
	turnBuf   map[string][]conversationTurn
	turnBufMu sync.Mutex
}

// ArchivistHookConfig holds optional dependencies.
type ArchivistHookConfig struct {
	DB           *sql.DB
	SegmentStore *segments.Store
	EventStore   *events.Store
	ProfileStore *profile.Store
	ChatProvider inference.Provider
	Model        string
	Embedder     embeddings.Provider
	Pipeline     MemoryPipelineIngestor
	Logger       *slog.Logger
}

// NewArchivistHook creates the archivist PostTurnHook.
func NewArchivistHook(cfg ArchivistHookConfig) *ArchivistHook {
	h := &ArchivistHook{
		db:             cfg.DB,
		enabled:        true,
		segmentStore:   cfg.SegmentStore,
		eventStore:     cfg.EventStore,
		profileStore:   cfg.ProfileStore,
		boundaryConfig: segments.DefaultBoundaryConfig(),
		chatProvider:   cfg.ChatProvider,
		model:          cfg.Model,
		embedder:       cfg.Embedder,
		pipeline:       cfg.Pipeline,
		turnBuf:        make(map[string][]conversationTurn),
		log:            cfg.Logger,
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.model == "" {
		h.model = "default"
	}
	return h
}

func (h *ArchivistHook) Name() string { return "archivist" }

// OnTurnComplete processes a completed turn.
func (h *ArchivistHook) OnTurnComplete(ctx context.Context, snapshot *TurnSnapshot) {
	if !h.enabled || h.segmentStore == nil || snapshot.ThreadID == "" {
		return
	}

	sessionID := snapshot.ThreadID
	now := float64(time.Now().UnixNano()) / 1e9

	// 1. Get or create open segment for this session.
	seg, err := h.segmentStore.OpenSegmentForSession(sessionID)
	if err != nil {
		h.log.Warn("archivist: failed to get open segment", "error", err)
		return
	}
	if seg == nil {
		seg, err = h.segmentStore.OpenSegment(sessionID, "global", int64(snapshot.TotalRounds), snapshot.TotalRounds)
		if err != nil {
			h.log.Warn("archivist: failed to open segment", "error", err)
			return
		}
	}

	// 2. Build turn content for boundary detection.
	turnContent := snapshot.UserMessage
	if snapshot.Response != "" {
		turnContent += "\n" + snapshot.Response
	}

	// 3. Check boundary.
	newTurnEmbedding := h.embedTurn(turnContent)
	if segments.DetectBoundary(h.boundaryConfig, turnContent, seg, newTurnEmbedding, seg.Embedding, now) {
		// Close current segment.
		if err := h.segmentStore.CloseSegment(seg.SegmentID); err != nil {
			h.log.Warn("archivist: failed to close segment", "error", err)
		} else {
			// Process the closed segment asynchronously.
			go func(s *segments.Segment) {
				defer func() {
					if r := recover(); r != nil {
						h.log.Error("archivist: segment processing panicked", "panic", r)
					}
				}()
				h.processClosedSegment(ctx, s)
			}(seg)
		}

		// Open new segment.
		seg, err = h.segmentStore.OpenSegment(sessionID, "global", int64(snapshot.TotalRounds), snapshot.TotalRounds)
		if err != nil {
			h.log.Warn("archivist: failed to open new segment", "error", err)
			return
		}
	}

	// 4. Append turn to current segment.
	if err := h.segmentStore.AppendTurn(seg.SegmentID, int64(snapshot.TotalRounds), snapshot.TotalRounds); err != nil {
		h.log.Warn("archivist: failed to append turn", "error", err)
	}

	// 5. Update segment embedding centroid.
	if len(newTurnEmbedding) > 0 {
		seg.Embedding = segments.IncrementalMeanEmbedding(seg.Embedding, newTurnEmbedding, seg.TurnCount)
	}

	// 6. Heuristic event extraction (Tier A — always runs, cheap).
	events := events.ExtractEventsHeuristic(turnContent, seg.SegmentID, sessionID)
	for _, ev := range events {
		if err := h.eventStore.Insert(&ev); err != nil {
			h.log.Warn("archivist: failed to insert event", "error", err)
		}
	}

	// 7. Extract simple profile facets from user message (lightweight, no LLM).
	h.extractSimpleFacets(snapshot, seg.SegmentID)

	// 8. Accumulate turns for conversation-to-tree archival.
	h.accumulateTurn(sessionID, snapshot)
}

// processClosedSegment handles a segment that has been closed:
// produces LLM recap, then runs embed/index/extract/profile in parallel.
func (h *ArchivistHook) processClosedSegment(ctx context.Context, seg *segments.Segment) {
	h.log.Info("archivist: processing closed segment", "segment_id", seg.SegmentID, "turns", seg.TurnCount)

	// Produce recap (must complete first — all downstream steps depend on it).
	summary, keywords := h.produceRecap(ctx, seg)

	// After recap is ready, embed, index, archive, event extraction, and
	// profile extraction are independent and run concurrently.
	g, gCtx := errgroup.WithContext(ctx)

	// Embed summary + store it.
	g.Go(func() error {
		var segmentEmbedding []float32
		if h.embedder != nil && summary != "" {
			vecs, err := h.embedder.Embed(gCtx, []string{summary})
			if err == nil && len(vecs) > 0 {
				segmentEmbedding = vecs[0]
			}
		}
		if err := h.segmentStore.SummarizeSegment(seg.SegmentID, summary, keywords, segmentEmbedding); err != nil {
			h.log.Warn("archivist: failed to summarize segment", "error", err)
		}
		return nil // non-fatal
	})

	// Ingest segment summary into memory tree.
	g.Go(func() error {
		if h.pipeline != nil && summary != "" {
			_ = h.pipeline.IndexContent("segment:"+seg.SegmentID, summary)
		}
		return nil
	})

	// Trigger full entity extraction + knowledge graph via ArchiveConversation.
	g.Go(func() error {
		if h.pipeline != nil {
			_ = h.pipeline.ArchiveConversation(seg.SessionID)
		}
		return nil
	})

	// Archive cleaned conversation turns as a memory tree leaf.
	g.Go(func() error {
		if h.pipeline != nil {
			turns := h.flushTurns(seg.SessionID)
			if len(turns) > 0 {
				cleaned := cleanConversation(turns)
				composed := composeConversationMD(cleaned)
				if composed != "" {
					_ = h.pipeline.IndexContent("tree:conversation:"+seg.SegmentID, composed)
				}
			}
		}
		return nil
	})

	// Tier B event extraction (LLM-based, if provider available).
	g.Go(func() error {
		if h.chatProvider != nil && summary != "" {
			h.extractEventsLLM(gCtx, seg, summary)
		}
		return nil
	})

	// Extract profile facets from segment (LLM-based).
	g.Go(func() error {
		if h.chatProvider != nil && summary != "" {
			h.extractProfileFromSegment(gCtx, seg, summary)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		h.log.Warn("archivist: processClosedSegment group error", "error", err)
	}
}

// produceRecap generates a segment summary, with LLM or heuristic fallback.
func (h *ArchivistHook) produceRecap(ctx context.Context, seg *segments.Segment) (summary, keywords string) {
	if h.chatProvider == nil {
		return fmt.Sprintf("Conversation segment with %d turns", seg.TurnCount), ""
	}

	prompt := fmt.Sprintf(
		`Summarize this conversation segment in 2-3 sentences. Focus on the main topic, key decisions, and outcomes.
Then list 3-5 topic keywords separated by commas.

Segment has %d turns. Reply in format:
Summary: <summary>
Keywords: <keyword1>, <keyword2>, ...`,
		seg.TurnCount,
	)

	req := inference.ChatRequest{
		Model:       h.model,
		Messages:    []inference.Message{{Role: "user", Content: prompt}},
		MaxTokens:   256,
		Temperature: 0.3,
	}

	tokens, errs := h.chatProvider.Chat(ctx, req)
	var text strings.Builder
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			text.WriteString(tok.Text)
		case err, ok := <-errs:
			if ok && err != nil {
				h.log.Warn("archivist: recap LLM error", "error", err)
				return fmt.Sprintf("Conversation segment (%d turns)", seg.TurnCount), ""
			}
		case <-ctx.Done():
			return fmt.Sprintf("Conversation segment (%d turns)", seg.TurnCount), ""
		}
	}

	response := text.String()
	// Parse summary and keywords.
	summary = response
	if idx := strings.Index(response, "Keywords:"); idx >= 0 {
		summary = strings.TrimSpace(response[:idx])
		summary = strings.TrimPrefix(summary, "Summary:")
		summary = strings.TrimSpace(summary)
		keywords = strings.TrimSpace(response[idx+len("Keywords:"):])
	}
	summary = strings.TrimPrefix(summary, "Summary:")
	summary = strings.TrimSpace(summary)

	if summary == "" {
		summary = fmt.Sprintf("Conversation segment (%d turns)", seg.TurnCount)
	}
	return summary, keywords
}

// extractEventsLLM runs Tier B (LLM-based) event extraction on a closed segment.
func (h *ArchivistHook) extractEventsLLM(ctx context.Context, seg *segments.Segment, summary string) {
	prompt := fmt.Sprintf(
		`Extract key facts, decisions, commitments, and preferences from this conversation summary.
Return each as a JSON array with fields: event_type (fact/decision/commitment/preference), content (one sentence), confidence (0.0-1.0).

Summary: %s

Reply with ONLY the JSON array.`, summary,
	)

	req := inference.ChatRequest{
		Model:       h.model,
		Messages:    []inference.Message{{Role: "user", Content: prompt}},
		MaxTokens:   512,
		Temperature: 0.2,
	}

	tokens, errs := h.chatProvider.Chat(ctx, req)
	var text strings.Builder
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			text.WriteString(tok.Text)
		case err, ok := <-errs:
			if ok && err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
	// Parse LLM response and persist extracted events.
	raw := text.String()
	h.parseAndStoreEvents(ctx, seg.SegmentID, raw)
}

// llmExtractedEvent is the JSON shape returned by the LLM event extraction prompt.
type llmExtractedEvent struct {
	EventType  string  `json:"event_type"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
}

// parseAndStoreEvents attempts to parse the LLM JSON response and persist events.
func (h *ArchivistHook) parseAndStoreEvents(ctx context.Context, segmentID string, raw string) {
	var parsed []llmExtractedEvent
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		if start := strings.Index(raw, "["); start >= 0 {
			if end := strings.LastIndex(raw, "]"); end > start {
				if err2 := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err2 != nil {
					return
				}
			} else {
				return
			}
		} else {
			return
		}
	}
	for _, e := range parsed {
		if e.Content == "" {
			continue
		}
		if e.Confidence <= 0 {
			e.Confidence = 0.5
		}
		if e.EventType == "" {
			e.EventType = string(events.EventFact)
		}
		ev := &events.Event{
			SegmentID:  segmentID,
			EventType:  events.EventType(e.EventType),
			Content:    e.Content,
			Confidence: e.Confidence,
		}
		_ = h.eventStore.Insert(ev)
	}
}

// llmProfileFacet is the JSON shape returned by the LLM profile extraction prompt.
type llmProfileFacet struct {
	FacetType  string  `json:"facet_type"`
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// parseAndUpsertProfile attempts to parse the LLM JSON response and upsert profile facets.
func (h *ArchivistHook) parseAndUpsertProfile(ctx context.Context, raw string) {
	var parsed []llmProfileFacet
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		if start := strings.Index(raw, "["); start >= 0 {
			if end := strings.LastIndex(raw, "]"); end > start {
				if err2 := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err2 != nil {
					return
				}
			} else {
				return
			}
		} else {
			return
		}
	}
	now := float64(time.Now().UnixNano()) / 1e9
	for _, f := range parsed {
		if f.Key == "" || f.Value == "" {
			continue
		}
		if f.Confidence <= 0 {
			f.Confidence = 0.5
		}
		if f.FacetType == "" {
			f.FacetType = "context"
		}
		_ = h.profileStore.UpsertFacet(&profile.ProfileFacet{
			FacetType:  profile.FacetType(f.FacetType),
			Key:        f.Key,
			Value:      f.Value,
			Confidence: f.Confidence,
			LastSeenAt: now,
		})
	}
}

// extractProfileFromSegment extracts user profile facets from a closed segment.
func (h *ArchivistHook) extractProfileFromSegment(ctx context.Context, seg *segments.Segment, summary string) {
	if h.profileStore == nil {
		return
	}

	prompt := fmt.Sprintf(
		`Extract facts about the user from this conversation summary. Return a JSON array with fields:
- facet_type: "preference", "skill", "role", "personality", or "context"
- key: short identifier (e.g., "preferred_language", "job_title")
- value: the extracted value
- confidence: 0.0-1.0

Summary: %s

Reply with ONLY the JSON array.`, summary,
	)

	req := inference.ChatRequest{
		Model:       h.model,
		Messages:    []inference.Message{{Role: "user", Content: prompt}},
		MaxTokens:   512,
		Temperature: 0.2,
	}

	tokens, errs := h.chatProvider.Chat(ctx, req)
	var text strings.Builder
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			text.WriteString(tok.Text)
		case err, ok := <-errs:
			if ok && err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
	// Parse LLM response and upsert profile facets.
	raw := text.String()
	h.parseAndUpsertProfile(ctx, raw)
}

// extractSimpleFacets extracts lightweight profile facets without LLM.
func (h *ArchivistHook) extractSimpleFacets(snapshot *TurnSnapshot, segmentID string) {
	if h.profileStore == nil {
		return
	}

	msg := strings.ToLower(snapshot.UserMessage)
	now := float64(time.Now().UnixNano()) / 1e9

	// Simple pattern-based facet extraction.
	type facetCheck struct {
		pattern    string
		facetType  profile.FacetType
		key        string
		extractVal func(string) string
	}
	checks := []facetCheck{
		{"i work at ", profile.FacetRole, "employer", func(s string) string { return extractAfter(s, "i work at ") }},
		{"i am a ", profile.FacetRole, "job_title", func(s string) string { return extractAfter(s, "i am a ") }},
		{"i'm a ", profile.FacetRole, "job_title", func(s string) string { return extractAfter(s, "i'm a ") }},
		{"my name is ", profile.FacetContext, "name", func(s string) string { return extractAfter(s, "my name is ") }},
		{"i live in ", profile.FacetContext, "location", func(s string) string { return extractAfter(s, "i live in ") }},
		{"i prefer ", profile.FacetPreference, "general_preference", func(s string) string { return extractAfter(s, "i prefer ") }},
		{"i use ", profile.FacetSkill, "tool", func(s string) string { return extractAfter(s, "i use ") }},
	}

	for _, c := range checks {
		if idx := strings.Index(msg, c.pattern); idx >= 0 {
			val := c.extractVal(msg[idx:])
			if val != "" && len(val) < 200 {
				h.profileStore.UpsertFacet(&profile.ProfileFacet{
					FacetType:        c.facetType,
					Key:              c.key,
					Value:            val,
					Confidence:       0.5,
					SourceSegmentIDs: segmentID,
					LastSeenAt:       now,
				})
			}
		}
	}
}

func extractAfter(s, prefix string) string {
	rest := s[len(prefix):]
	// Take up to first punctuation or 80 chars.
	for i, r := range rest {
		if r == '.' || r == ',' || r == '!' || r == '?' || r == '\n' {
			return strings.TrimSpace(rest[:i])
		}
		if i > 80 {
			return strings.TrimSpace(rest[:i])
		}
	}
	return strings.TrimSpace(rest)
}

// embedTurn produces an embedding for a turn's content if an embedder is available.
func (h *ArchivistHook) embedTurn(content string) []float32 {
	if h.embedder == nil || content == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vecs, err := h.embedder.Embed(ctx, []string{content})
	if err != nil || len(vecs) == 0 {
		return nil
	}
	return vecs[0]
}

// FlushOpenSegment force-closes the open segment for a session at shutdown.
func (h *ArchivistHook) FlushOpenSegment(sessionID string) {
	if h.segmentStore == nil {
		return
	}
	seg, err := h.segmentStore.OpenSegmentForSession(sessionID)
	if err != nil || seg == nil {
		return
	}
	if err := h.segmentStore.CloseSegment(seg.SegmentID); err != nil {
		h.log.Warn("archivist: failed to flush segment", "error", err)
		return
	}
	h.processClosedSegment(context.Background(), seg)
}

// FlushAllOpenSegments closes all open segments (call at shutdown).
func (h *ArchivistHook) FlushAllOpenSegments() {
	// List all open segments via the segment store query.
	segs, err := h.segmentStore.SegmentsPendingSummary(100)
	if err != nil {
		h.log.Warn("archivist: failed to list pending segments", "error", err)
		return
	}
	for _, seg := range segs {
		if seg.Status == segments.StatusClosed || seg.Status == segments.StatusOpen {
			if seg.Status == segments.StatusOpen {
				h.segmentStore.CloseSegment(seg.SegmentID)
			}
			h.processClosedSegment(context.Background(), &seg)
		}
	}
}

// ── Conversation-to-tree archival ───────────────────────────────────────
// Mirrors Rust memory_archivist: clip → compose → archive_to_tree pipeline.

// maxTurnBufPerSession caps the per-session turn buffer at ~50 round-trips
// to prevent unbounded growth when a session never closes a segment.
const maxTurnBufPerSession = 100

// accumulateTurn buffers a turn's user message and assistant response.
func (h *ArchivistHook) accumulateTurn(sessionID string, snapshot *TurnSnapshot) {
	h.turnBufMu.Lock()
	defer h.turnBufMu.Unlock()

	// Guard against unbounded growth from sessions that never close a segment.
	if len(h.turnBuf[sessionID]) >= maxTurnBufPerSession {
		return
	}

	now := time.Now().UnixMilli()
	if snapshot.UserMessage != "" {
		h.turnBuf[sessionID] = append(h.turnBuf[sessionID], conversationTurn{
			Role: "user", Content: snapshot.UserMessage, Timestamp: now,
		})
	}
	if snapshot.Response != "" {
		h.turnBuf[sessionID] = append(h.turnBuf[sessionID], conversationTurn{
			Role: "assistant", Content: snapshot.Response, Timestamp: now,
		})
	}
}

// flushTurns drains and returns the turn buffer for a session.
func (h *ArchivistHook) flushTurns(sessionID string) []conversationTurn {
	h.turnBufMu.Lock()
	defer h.turnBufMu.Unlock()
	turns := h.turnBuf[sessionID]
	delete(h.turnBuf, sessionID)
	return turns
}

// cleanConversation strips tool-role turns.
func cleanConversation(turns []conversationTurn) []conversationTurn {
	filtered := make([]conversationTurn, 0, len(turns))
	for _, t := range turns {
		if t.Role == "tool" || t.Content == "" {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// composeConversationMD composes turns into markdown.
func composeConversationMD(turns []conversationTurn) string {
	if len(turns) == 0 {
		return ""
	}
	var b strings.Builder
	for i, t := range turns {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("## ")
		b.WriteString(t.Role)
		b.WriteByte('\n')
		b.WriteString(t.Content)
		if !strings.HasSuffix(t.Content, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
