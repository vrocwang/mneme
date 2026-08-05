package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/simon/mneme/internal/embeddings"
	"github.com/simon/mneme/internal/memory/archivist"
	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/memory/entities"
	"github.com/simon/mneme/internal/memory/graph"
	"github.com/simon/mneme/internal/memory/queue"
	"github.com/simon/mneme/internal/memory/store"
	"github.com/simon/mneme/internal/memory/tree"
)

// Pipeline orchestrates the memory processing pipeline.
type Pipeline struct {
	log            *slog.Logger
	convStore      *conversations.Store
	memStore       *store.Store
	memTree        *tree.Tree
	queue          *queue.Queue
	embedder       embeddings.Provider     // optional — when set, enables vector search
	arch           *archivist.Archivist    // optional — when set, LLM-summarizes chunks
	retriever      *MultiStrategyRetriever // optional — when set, multi-strategy search
	entityReg      *entities.Registry      // optional — entity extraction and storage
	entityGraph    *graph.Graph            // optional — entity co-occurrence graph
	entityEnricher entities.Enricher       // optional — LLM entity enrichment
	redactor       *Redactor               // PII/secret redaction before storage
}

func NewPipeline(log *slog.Logger, convStore *conversations.Store, memStore *store.Store, db *sql.DB) *Pipeline {
	cfg := queue.DefaultConfig(db)
	cfg.WorkerCount = 2

	// Use a persistent tree when a DB is available, so memory survives restarts.
	var memTree *tree.Tree
	if db != nil {
		var err error
		memTree, err = tree.NewPersistentTree(10, db)
		if err != nil {
			log.Warn("failed to create persistent memory tree, using in-memory fallback", "error", err)
			memTree = tree.NewTree(10)
		}
	} else {
		memTree = tree.NewTree(10)
	}

	if db != nil {
		if err := queue.Migrate(db); err != nil {
			log.Warn("failed to migrate queue table", "error", err)
		}
	}

	return &Pipeline{
		log:       log,
		convStore: convStore,
		memStore:  memStore,
		memTree:   memTree,
		queue:     queue.New(cfg),
		redactor:  NewRedactor(),
	}
}

// WithEmbedder sets an embeddings provider for vector search and
// initializes the multi-strategy retriever if not already set.
func (p *Pipeline) WithEmbedder(e embeddings.Provider) *Pipeline {
	p.embedder = e
	if p.retriever == nil {
		p.retriever = NewMultiStrategyRetriever(p.memStore, p.memTree, embeddingAdapter{e}, DefaultWeights(), p.log)
		if p.entityGraph != nil {
			p.retriever.WithGraphScorer(&pipelineGraphScorer{graph: p.entityGraph, entityReg: p.entityReg, embedder: p.embedder})
		}
		p.retriever.WithEpisodicScorer(&episodicAdapter{convStore: p.convStore})
	}
	return p
}

// embeddingAdapter adapts embeddings.Provider to the retriever's interface.
type embeddingAdapter struct{ embeddings.Provider }

func (a embeddingAdapter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return a.Provider.Embed(ctx, texts)
}
func (a embeddingAdapter) Name() string    { return a.Provider.Name() }
func (a embeddingAdapter) Dimensions() int { return a.Provider.Dimensions() }

// SetEntityEnricher configures an optional LLM-based entity enricher.
func (p *Pipeline) SetEntityEnricher(e entities.Enricher) { p.entityEnricher = e }

// WithArchivist sets an archivist for LLM-based chunk summarization during archival.
func (p *Pipeline) WithArchivist(a *archivist.Archivist) *Pipeline {
	p.arch = a
	return p
}

// WithEntities sets an entity registry and graph for entity extraction and
// co-occurrence tracking during archival. Graph edges boost retrieval relevance.
func (p *Pipeline) WithEntities(reg *entities.Registry, g *graph.Graph) *Pipeline {
	p.entityReg = reg
	p.entityGraph = g
	return p
}

// InitEntities creates the entity registry and knowledge graph, and wires them
// into the pipeline. Non-fatal: logs warnings but returns nil on error.
func (p *Pipeline) InitEntities(workspace string, db *sql.DB) error {
	entitiesDir := filepath.Join(workspace, "memory", "entities")
	reg, err := entities.NewRegistry(entitiesDir)
	if err != nil {
		return fmt.Errorf("entity registry: %w", err)
	}
	g, err := graph.New(db)
	if err != nil {
		return fmt.Errorf("entity graph: %w", err)
	}
	p.WithEntities(reg, g)
	p.log.Info("entity extraction and knowledge graph enabled")
	return nil
}

// Start begins the async pipeline workers. Call Migrate first on the queue's DB.
func (p *Pipeline) Start() {
	p.queue.RegisterHandler(queue.KindExtractChunk, p.handleArchive)
	p.queue.RegisterHandler(queue.KindAppendBuffer, p.handleIndex)
	p.queue.Start()
	p.log.Info("memory pipeline started")
}

// ReembedBackfill re-embeds all chunks whose embedding model differs from the
// current embedder. Call this after changing the embedding model to keep
// vector search accurate.
func (p *Pipeline) ReembedBackfill(ctx context.Context) (int, error) {
	if p.embedder == nil {
		return 0, nil
	}
	currentModel := fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
	chunks, err := p.memStore.ListByModel(ctx, currentModel)
	if err != nil {
		return 0, fmt.Errorf("backfill list: %w", err)
	}
	count := 0
	for _, c := range chunks {
		vecs, embErr := p.embedder.Embed(ctx, []string{c.Content})
		if embErr != nil {
			p.log.Warn("backfill embed failed", "chunk_id", c.ID, "error", embErr)
			continue
		}
		if len(vecs) > 0 {
			c.Vector = vecs[0]
			c.EmbeddingModel = currentModel
			if updateErr := p.memStore.UpdateChunk(ctx, c); updateErr != nil {
				p.log.Warn("backfill update failed", "chunk_id", c.ID, "error", updateErr)
				continue
			}
			count++
		}
	}
	p.log.Info("backfill complete", "total", len(chunks), "reembedded", count)
	return count, nil
}

// Stop shuts down the pipeline.
func (p *Pipeline) Stop() {
	p.queue.Stop()
	p.log.Info("memory pipeline stopped")
}

// ── Job payload types ──────────────────────────────────────────────

type archivePayload struct {
	ThreadID string `json:"thread_id"`
}

type indexPayload struct {
	Source  string `json:"source"`
	Content string `json:"content"`
	Taint   string `json:"taint,omitempty"` // "external_sync" for sync sources, empty => default "internal"
}

// ── Submit ─────────────────────────────────────────────────────────

// ArchiveConversation submits a conversation archiving job. Returns an error
// if marshalling fails or the queue rejects the job.
func (p *Pipeline) ArchiveConversation(threadID string) error {
	payload := archivePayload{ThreadID: threadID}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal archive payload: %w", err)
	}
	dedupeKey := fmt.Sprintf("archive:%s", threadID)
	if _, err := p.queue.Enqueue(queue.KindExtractChunk, string(payloadJSON), dedupeKey, time.Time{}); err != nil {
		return fmt.Errorf("enqueue archive job: %w", err)
	}
	return nil
}

// Store returns the underlying memory store for tools that need direct
// access (e.g. MemoryDiffTool for snapshot/diff operations).
func (p *Pipeline) Store() *store.Store { return p.memStore }

// ForgetContent removes memory chunks whose content contains the given
// substring. Returns the number of chunks deleted.
func (p *Pipeline) ForgetContent(substr string) (int64, error) {
	if p.memStore == nil {
		return 0, fmt.Errorf("memory store not available")
	}
	return p.memStore.DeleteByContent(substr)
}

// HasExternalContent returns true when the store contains memory chunks with
// TaintExternalSync created since the specified time. A zero since means all
// time. Used by the subconscious engine to decide whether to scope the turn
// origin as SubconsciousTainted, matching Rust's per-tick situation_report
// time-windowed taint detection.
func (p *Pipeline) HasExternalContent(ctx context.Context, since time.Time) bool {
	if p.memStore == nil {
		return false
	}
	var count int
	var err error
	if since.IsZero() {
		count, err = p.memStore.CountByTaint(ctx, store.TaintExternalSync)
	} else {
		count, err = p.memStore.CountByTaintSince(ctx, store.TaintExternalSync, since)
	}
	if err != nil {
		return false
	}
	return count > 0
}

// DiffService returns a MemoryDiff service backed by this pipeline's store,
// for snapshot/diff operations triggered by the sync scheduler.
func (p *Pipeline) DiffService() *MemoryDiff {
	if p.memStore == nil {
		return nil
	}
	return NewMemoryDiff(p.memStore)
}

// IndexContent submits an indexing job with Internal taint.
// Use IndexContentWithTaint for external sync sources.
func (p *Pipeline) IndexContent(source, content string) error {
	return p.IndexContentWithTaint(source, content, "")
}

// IndexContentWithTaint submits an indexing job with an explicit taint value.
// When taint is "external_sync" the memory is treated as externally sourced
// and gated more strictly during subconscious automation ticks.
func (p *Pipeline) IndexContentWithTaint(source, content, taint string) error {
	payload := indexPayload{Source: source, Content: content, Taint: taint}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal index payload: %w", err)
	}
	contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(source+content)))[:16]
	dedupeKey := fmt.Sprintf("index:%s:%s", source, contentHash)
	if _, err := p.queue.Enqueue(queue.KindAppendBuffer, string(payloadJSON), dedupeKey, time.Time{}); err != nil {
		return fmt.Errorf("enqueue index job: %w", err)
	}
	return nil
}

// ── Handlers ───────────────────────────────────────────────────────

func (p *Pipeline) handleArchive(ctx context.Context, job queue.Job) (queue.JobOutcome, error) {
	var payload archivePayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return queue.JobOutcome{Done: true}, nil
	}
	threadID := payload.ThreadID
	if threadID == "" {
		return queue.JobOutcome{Done: true}, nil
	}

	msgs, err := p.convStore.GetMessages(threadID, 200)
	if err != nil {
		return queue.JobOutcome{}, err
	}

	if len(msgs) == 0 {
		return queue.JobOutcome{Done: true}, nil
	}

	// Combine messages into a document
	var doc string
	for _, m := range msgs {
		doc += fmt.Sprintf("[%s]: %s\n", m.Role, m.Content)
	}

	// Index into memory store with optional embedding.
	// Call the archivist once and reuse the result for both the store summary and tree sealing.
	summary := fmt.Sprintf("Conversation %s (%d messages)", threadID, len(msgs))
	var archivistResult *archivist.SummaryResult
	if p.arch != nil {
		if result, err := p.arch.SummarizeMemory(ctx, doc); err == nil && result != nil {
			archivistResult = result
			summary = result.Summary
			if !result.ShouldPrune && len(result.KeyFacts) > 0 {
				doc += "\n\nKey facts:\n" + strings.Join(result.KeyFacts, "\n")
			}
		}
	}
	// Redact PII and secrets before storage.
	if p.redactor != nil {
		redacted, found := p.redactor.Redact(doc)
		if len(found) > 0 {
			p.log.Debug("redacted PII from archive", "thread_id", threadID, "patterns", found)
		}
		doc = redacted
	}

	chunk := store.MemoryChunk{
		Source:  "conversation",
		Content: doc,
		Summary: summary,
	}
	chunk, alreadyInserted := p.embedIfAvailable(ctx, chunk)

	var id int64
	if !alreadyInserted {
		var err error
		id, err = p.memStore.Insert(chunk)
		if err != nil {
			return queue.JobOutcome{}, err
		}
	}

	// Extract entities and record co-occurrences in the knowledge graph.
	if p.entityReg != nil {
		extracted := entities.ExtractFromText(doc)
		if p.entityEnricher != nil && len(extracted) > 0 {
			if enriched, err := p.entityEnricher.Enrich(ctx, extracted); err == nil {
				extracted = enriched
			}
		}
		names := make([]string, 0, len(extracted))
		for _, e := range extracted {
			if e.Name != "" {
				p.entityReg.Upsert(e)
				names = append(names, e.Name)
			}
		}
		if p.entityGraph != nil && len(names) >= 2 {
			// Extract S-P-O relations and add subject+object as co-occurrences.
			relations := entities.ExtractRelations(doc)
			for _, rel := range relations {
				if rel.Subject != "" && rel.Object != "" {
					names = append(names, rel.Subject, rel.Object)
				}
			}
			p.entityGraph.RecordCoOccurrencePersisted(names)
		}
	}
	// Add to memory tree
	nodeID := fmt.Sprintf("conv-%s", threadID)
	node, _ := p.memTree.Add("root", nodeID, doc)
	// Seal the tree node if it has accumulated enough content.
	if node != nil && node.Count >= p.memTree.BucketSize() {
		if archivistResult != nil {
			p.memTree.Seal(nodeID, archivistResult.Summary)
		} else {
			p.memTree.Seal(nodeID, summary)
		}
	}

	p.log.Info("archived conversation", "thread_id", threadID, "messages", len(msgs), "chunk_id", id)
	return queue.JobOutcome{Done: true}, nil
}

func (p *Pipeline) handleIndex(ctx context.Context, job queue.Job) (queue.JobOutcome, error) {
	var payload indexPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return queue.JobOutcome{Done: true}, nil
	}
	source := payload.Source
	content := payload.Content
	if content == "" {
		return queue.JobOutcome{Done: true}, nil
	}

	// Redact PII and secrets before storage.
	if p.redactor != nil {
		redacted, found := p.redactor.Redact(content)
		if len(found) > 0 {
			p.log.Debug("redacted PII from index", "source", source, "patterns", found)
		}
		content = redacted
	}

	chunk := store.MemoryChunk{
		Source:  source,
		Content: content,
	}
	if payload.Taint == "external_sync" {
		chunk.Taint = store.TaintExternalSync
	}
	chunk, alreadyInserted := p.embedIfAvailable(ctx, chunk)

	var id int64
	if !alreadyInserted {
		var err error
		id, err = p.memStore.Insert(chunk)
		if err != nil {
			return queue.JobOutcome{}, err
		}
	}

	// Extract entities and record co-occurrences in the knowledge graph.
	// This mirrors handleArchive's entity extraction so that IndexContent
	// callers (ArchivistHook, external sync, etc.) also populate the
	// entity registry and knowledge graph.
	if p.entityReg != nil {
		extracted := entities.ExtractFromText(content)
		names := make([]string, 0, len(extracted))
		for _, e := range extracted {
			if e.Name != "" {
				p.entityReg.Upsert(e)
				names = append(names, e.Name)
			}
		}
		if p.entityGraph != nil && len(names) >= 2 {
			relations := entities.ExtractRelations(content)
			for _, rel := range relations {
				if rel.Subject != "" && rel.Object != "" {
					names = append(names, rel.Subject, rel.Object)
				}
			}
			p.entityGraph.RecordCoOccurrencePersisted(names)
		}
	}

	p.log.Info("indexed content", "source", source, "chunk_id", id)
	return queue.JobOutcome{Done: true}, nil
}

// embedIfAvailable computes an embedding for the chunk content when an
// embedder is configured. When the content exceeds maxChunkChars, it is
// split into sub-chunks that are each embedded separately and inserted
// as individual memory chunks. This prevents embedding model context
// window overflow (e.g. nomic-embed-text has an 8192-token limit).
//
// Returns (chunk, alreadyInserted). When alreadyInserted is true the
// caller must skip its own Insert because sub-chunks were inserted inline.
func (p *Pipeline) embedIfAvailable(ctx context.Context, chunk store.MemoryChunk) (store.MemoryChunk, bool) {
	if p.embedder == nil {
		return chunk, false
	}
	// Split long content into sub-chunks and insert them individually.
	if utf8.RuneCountInString(chunk.Content) > maxChunkChars {
		sub := chunkContent(chunk.Content, maxChunkChars)
		anyInserted := false
		for i, s := range sub {
			subChunk := chunk
			subChunk.Content = s
			if i > 0 {
				subChunk.Summary = "" // only the first chunk keeps the summary
			}
			vecs, err := p.embedder.Embed(ctx, []string{s})
			if err != nil {
				p.log.Warn("embedding failed for sub-chunk", "error", err)
				continue
			}
			if len(vecs) > 0 {
				subChunk.Vector = vecs[0]
				subChunk.EmbeddingModel = fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
			}
			if _, insErr := p.memStore.Insert(subChunk); insErr != nil {
				p.log.Warn("sub-chunk insert failed", "error", insErr)
			} else {
				anyInserted = true
			}
		}
		if !anyInserted {
			// No sub-chunk was inserted; let the caller insert the
			// original (un-embedded) chunk so the content is not lost.
			return chunk, false
		}
		// Signal to the caller that sub-chunks were already inserted.
		// The returned chunk is a truncated marker — do not re-insert.
		runes := []rune(chunk.Content)
		if len(runes) > maxChunkChars {
			chunk.Content = string(runes[:maxChunkChars]) + "\n... [truncated]"
		}
		return chunk, true
	}

	vecs, err := p.embedder.Embed(ctx, []string{chunk.Content})
	if err != nil {
		p.log.Warn("embedding failed for chunk, storing without vector", "error", err)
		return chunk, false
	}
	if len(vecs) > 0 {
		chunk.Vector = vecs[0]
		chunk.EmbeddingModel = fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
	}
	return chunk, false
}

// maxChunkChars is the maximum characters per memory chunk before splitting.
// At ~4 chars/token, 2000 chars ≈ 500 tokens, safe for any embedding model.
const maxChunkChars = 2000

// chunkContent splits content into sub-chunks of at most maxLen runes,
// preferring to split at paragraph boundaries. Using runes prevents
// splitting multi-byte UTF-8 characters. Separator searches are performed
// on the rune slice to avoid byte/rune offset confusion.
func chunkContent(content string, maxLen int) []string {
	runes := []rune(content)
	if len(runes) <= maxLen {
		return []string{content}
	}
	var chunks []string
	remaining := runes
	for len(remaining) > maxLen {
		cut := maxLen
		if idx := lastRuneIndex(remaining[:maxLen], []rune("\n\n")); idx > maxLen/2 {
			cut = idx + 2
		} else if idx := lastRuneIndex(remaining[:maxLen], []rune("\n")); idx > maxLen/2 {
			cut = idx + 1
		} else if idx := lastRuneIndex(remaining[:maxLen], []rune(". ")); idx > maxLen/2 {
			cut = idx + 2
		}
		chunks = append(chunks, string(remaining[:cut]))
		remaining = remaining[cut:]
	}
	if len(remaining) > 0 {
		chunks = append(chunks, string(remaining))
	}
	return chunks
}

// lastRuneIndex returns the start index of the last occurrence of sep in
// runes, or -1 when not found. Both haystack and sep are []rune so the
// returned index is a rune offset safe to use on rune slices.
func lastRuneIndex(haystack, sep []rune) int {
	if len(sep) == 0 || len(haystack) < len(sep) {
		return -1
	}
	for i := len(haystack) - len(sep); i >= 0; i-- {
		match := true
		for j := range sep {
			if haystack[i+j] != sep[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Search performs hybrid search across memory store and tree.
// When a multi-strategy retriever is configured, it combines FTS5, vector,
// keyword, and tree signals with configurable weights.
// TreeContext returns namespace-level memory summaries from tree root
// children for injection into the system prompt. Returns empty string when
// no summaries are available.
func (p *Pipeline) TreeContext() string {
	if p.memTree == nil {
		return ""
	}
	summaries := p.memTree.RootSummaries()
	if len(summaries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Memory context:\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("- %s\n", s))
	}
	return b.String()
}

// TreeSummary is a lightweight view of a tree node for external consumers.
type TreeSummary struct {
	ID      string
	Content string
	Summary string
	Count   int
}

// TreeRootSummaries returns structured root summaries for context injection.
func (p *Pipeline) TreeRootSummaries() []TreeSummary {
	if p.memTree == nil {
		return nil
	}
	strs := p.memTree.RootSummaries()
	out := make([]TreeSummary, len(strs))
	for i, s := range strs {
		out[i] = TreeSummary{ID: "root", Summary: s, Count: 1}
	}
	return out
}

// TreeSearch searches the memory tree for nodes matching a query.

// SearchWithFilter performs a memory search with an optional signal filter.
// filter: "all", "fts5", "vector", "graph", or "" (same as "all").
func (p *Pipeline) SearchWithFilter(ctx context.Context, query string, limit int, filter string) (*SearchResult, error) {
	if p.retriever == nil || filter == "" || filter == "all" {
		return p.Search(ctx, query, limit)
	}

	origWeights := p.retriever.Weights()
	filteredWeights := origWeights
	switch filter {
	case "fts5":
		filteredWeights.Vector = 0
		filteredWeights.Tree = 0
		filteredWeights.Graph = 0
	case "vector":
		filteredWeights.FTS5 = 0
		filteredWeights.Tree = 0
		filteredWeights.Graph = 0
	case "graph":
		filteredWeights.FTS5 = 0
		filteredWeights.Vector = 0
		filteredWeights.Tree = 0
	}
	scored, err := p.retriever.SearchWeighted(ctx, query, limit, filteredWeights)
	if err != nil {
		return nil, fmt.Errorf("multi-strategy search: %w", err)
	}
	chunks := make([]store.MemoryChunk, len(scored))
	for i, s := range scored {
		chunks[i] = s.Chunk
	}
	return &SearchResult{Query: query, Chunks: chunks, Scored: scored}, nil
}

// ApplyRetrievalProfile sets retriever weights from a named profile.
func (p *Pipeline) ApplyRetrievalProfile(profile string) {
	if p.retriever == nil || profile == "" {
		return
	}
	p.retriever.ApplyProfile(WeightProfile(profile))
}
func (p *Pipeline) TreeSearchNodes(query string, limit int) []TreeSummary {
	if p.memTree == nil {
		return nil
	}
	nodes := p.memTree.Search(query, limit)
	out := make([]TreeSummary, len(nodes))
	for i, n := range nodes {
		out[i] = TreeSummary{ID: n.ID, Content: n.Content, Summary: n.Summary, Count: n.Count}
	}
	return out
}

// SearchCrossSession searches the episodic log across all sessions, excluding
// the given thread ID. Returns formatted episodic results.
func (p *Pipeline) SearchCrossSession(query string, excludeThreadID string, limit int) ([]conversations.EpisodicResult, error) {
	if p.convStore == nil {
		return nil, nil
	}
	return p.convStore.SearchEpisodic(query, excludeThreadID, limit)
}
func (p *Pipeline) Search(ctx context.Context, query string, limit int) (*SearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Use multi-strategy retriever when available.
	if p.retriever != nil {
		scored, err := p.retriever.Search(ctx, query, limit)
		if err != nil {
			return nil, fmt.Errorf("multi-strategy search: %w", err)
		}
		chunks := make([]store.MemoryChunk, len(scored))
		for i, s := range scored {
			chunks[i] = s.Chunk
		}
		return &SearchResult{Query: query, Chunks: chunks, Scored: scored}, nil
	}

	// Fallback: basic FTS5 + vector + tree search.
	var chunks []store.MemoryChunk
	var err error
	if p.embedder != nil {
		vecs, embErr := p.embedder.Embed(ctx, []string{query})
		if embErr == nil && len(vecs) > 0 {
			modelSig := fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
			chunks, err = p.memStore.HybridSearch(query, vecs[0], limit, modelSig)
			if err == nil {
				goto treeSearch
			}
		}
	}
	chunks, err = p.memStore.Search(query, limit)
	if err != nil {
		return nil, fmt.Errorf("store search: %w", err)
	}

treeSearch:
	nodes := p.memTree.Search(query, limit)
	return &SearchResult{Query: query, Chunks: chunks, Nodes: nodes}, nil
}

// SearchResult combines store and tree results.
type SearchResult struct {
	Query  string
	Chunks []store.MemoryChunk
	Nodes  []*tree.Node
	Scored []ScoredChunk // populated when multi-strategy retriever is active
}

// TotalResults returns the combined result count.
func (r *SearchResult) TotalResults() int {
	if len(r.Scored) > 0 {
		return len(r.Scored)
	}
	return len(r.Chunks) + len(r.Nodes)
}

// Formatted returns search results as a readable string.
func (r *SearchResult) Formatted() string {
	if len(r.Scored) > 0 {
		return FormatScoredResults(r.Scored)
	}

	var out string
	out += fmt.Sprintf("Search results for: %q\n\n", r.Query)
	if len(r.Chunks) > 0 {
		out += "=== Memory Store ===\n"
		for _, c := range r.Chunks {
			out += fmt.Sprintf("- [%s] %s\n", c.Source, truncate(c.Content, 200))
		}
		out += "\n"
	}
	if len(r.Nodes) > 0 {
		out += "=== Memory Tree ===\n"
		for _, n := range r.Nodes {
			out += fmt.Sprintf("- [%s] %s\n", n.ID, truncate(n.Content, 200))
		}
	}
	if r.TotalResults() == 0 {
		out += "No results found.\n"
	}
	return out
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

type pipelineGraphScorer struct {
	graph     *graph.Graph
	entityReg *entities.Registry
	embedder  embeddings.Provider
}

func (gs *pipelineGraphScorer) GraphScore(query string, chunkContent string, chunkVector []float32) float64 {
	if gs.graph == nil {
		return 0
	}
	queryEntities := entities.ExtractFromText(query)
	chunkEntities := entities.ExtractFromText(chunkContent)
	if len(queryEntities) == 0 || len(chunkEntities) == 0 {
		return 0
	}

	// Build a set of chunk entity names for fast lookup.
	chunkSet := make(map[string]float64, len(chunkEntities))
	for _, ce := range chunkEntities {
		chunkSet[ce.Name] = 1.0 // direct match bonus
	}

	var totalWeight float64
	var matches int
	for _, qe := range queryEntities {
		// Direct entity match: strongest signal.
		if _, ok := chunkSet[qe.Name]; ok {
			totalWeight += 1.0
			matches++
			continue
		}

		// Graph traversal: entities connected via co-occurrence.
		related := gs.graph.GetRelated(qe.Name, 10, 2)
		bestRelWeight := 0.0
		for _, re := range related {
			if w, ok := chunkSet[re.Source]; ok {
				bestRelWeight = math.Max(bestRelWeight, re.Weight*w)
			} else if w, ok := chunkSet[re.Target]; ok {
				bestRelWeight = math.Max(bestRelWeight, re.Weight*w)
			}
		}
		if bestRelWeight > 0 {
			totalWeight += bestRelWeight
			matches++
		}
	}
	if matches == 0 && gs.embedder == nil {
		return 0
	}

	entityScore := totalWeight / float64(max(len(queryEntities), 1))
	if entityScore > 1.0 {
		entityScore = 1.0
	}

	// Blend in embedding similarity when vectors are available.
	if gs.embedder != nil && len(chunkVector) > 0 && gs.embedder.Dimensions() > 0 {
		queryVecs, err := gs.embedder.Embed(context.Background(), []string{query})
		if err == nil && len(queryVecs) > 0 && len(queryVecs[0]) == len(chunkVector) {
			vecScore := cosineSimilarity(queryVecs[0], chunkVector)
			if vecScore < 0 {
				vecScore = 0
			}
			// Weighted blend: 40% entity + 60% vector, or full vector when no entity match.
			if matches == 0 {
				return vecScore
			}
			return 0.4*entityScore + 0.6*vecScore
		}
	}
	if matches == 0 {
		return 0
	}
	return entityScore
}

// episodicAdapter makes conversations store available to the retriever.
type episodicAdapter struct {
	convStore *conversations.Store
}

func (ea *episodicAdapter) EpisodicScore(query string, limit int) float64 {
	if ea.convStore == nil {
		return 0
	}
	results, err := ea.convStore.SearchEpisodic(query, "", limit)
	if err != nil || len(results) == 0 {
		return 0
	}
	return float64(len(results)) / float64(limit)
}
