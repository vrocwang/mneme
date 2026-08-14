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
	"github.com/simon/mneme/internal/memory/profile"
	"github.com/simon/mneme/internal/memory/queue"
	"github.com/simon/mneme/internal/memory/store"
	"github.com/simon/mneme/internal/memory/tree"
)

// Pipeline orchestrates the memory processing pipeline.
type Pipeline struct {
	log            *slog.Logger
	convStore      *conversations.Store
	memStore       *store.Store
	layered        *store.LayeredStore // optional — L1 atoms / L2 scenarios (layered model)
	memTree        *tree.Tree
	queue          *queue.Queue
	embedder       embeddings.Provider     // optional — when set, enables vector search
	arch           *archivist.Archivist    // optional — when set, LLM-summarizes chunks
	retriever      *MultiStrategyRetriever // optional — when set, multi-strategy search
	entityReg      *entities.Registry      // optional — entity extraction and storage
	entityGraph    *graph.Graph            // optional — entity co-occurrence graph
	entityEnricher entities.Enricher       // optional — LLM entity enrichment
	profileStore   *profile.Store          // optional — L3 persona facet store
	redactor       *Redactor               // PII/secret redaction before storage

	archiveMsgLimit   int     // max messages archived per conversation (0 = default)
	maxChunkChars     int     // max runes per memory chunk before splitting (0 = default)
	freshnessHalfLife float64 // hours for freshness decay half-life (0 = default)
	maxSearchResults  int     // default search result limit (0 = default)
}

// PipelineConfig carries runtime tunables for the pipeline that were
// previously hardcoded. Zero values fall back to the built-in defaults.
type PipelineConfig struct {
	WorkerCount       int
	TreeBucketSize    int
	ArchiveMsgLimit   int
	MaxChunkSize      int
	MaxSearchResults  int
	FreshnessHalfLife float64
}

const (
	defaultArchiveMsgLimit   = 200
	defaultMaxChunkChars     = 2000
	defaultMaxSearchResults  = 20
	defaultFreshnessHalfLife = 168.0 // 7 days in hours
)

func NewPipeline(log *slog.Logger, convStore *conversations.Store, memStore *store.Store, db *sql.DB) *Pipeline {
	return NewPipelineWithConfig(log, convStore, memStore, db, PipelineConfig{})
}

// NewPipelineWithConfig is NewPipeline with explicit runtime tunables.
func NewPipelineWithConfig(log *slog.Logger, convStore *conversations.Store, memStore *store.Store, db *sql.DB, pc PipelineConfig) *Pipeline {
	if pc.WorkerCount <= 0 {
		pc.WorkerCount = 2
	}
	if pc.TreeBucketSize <= 0 {
		pc.TreeBucketSize = 10
	}
	if pc.ArchiveMsgLimit <= 0 {
		pc.ArchiveMsgLimit = defaultArchiveMsgLimit
	}
	if pc.MaxChunkSize <= 0 {
		pc.MaxChunkSize = defaultMaxChunkChars
	}
	if pc.MaxSearchResults <= 0 {
		pc.MaxSearchResults = defaultMaxSearchResults
	}
	if pc.FreshnessHalfLife <= 0 {
		pc.FreshnessHalfLife = defaultFreshnessHalfLife
	}

	cfg := queue.DefaultConfig(db)
	cfg.WorkerCount = pc.WorkerCount

	// Use a persistent tree when a DB is available, so memory survives restarts.
	var memTree *tree.Tree
	if db != nil {
		var err error
		memTree, err = tree.NewPersistentTree(pc.TreeBucketSize, db)
		if err != nil {
			log.Warn("failed to create persistent memory tree, using in-memory fallback", "error", err)
			memTree = tree.NewTree(pc.TreeBucketSize)
		}
	} else {
		memTree = tree.NewTree(pc.TreeBucketSize)
	}

	if db != nil {
		if err := queue.Migrate(db); err != nil {
			log.Warn("failed to migrate queue table", "error", err)
		}
	}

	// Layered store for the L0-L3 pyramid. Non-fatal: if creation fails we
	// keep running with the legacy flat store only (L1/L2 extraction no-ops).
	var layered *store.LayeredStore
	if db != nil {
		if ls, err := store.NewLayeredStore(db); err != nil {
			log.Warn("failed to create layered memory store, L1/L2 disabled", "error", err)
		} else {
			layered = ls
		}
	}

	return &Pipeline{
		log:               log,
		convStore:         convStore,
		memStore:          memStore,
		layered:           layered,
		memTree:           memTree,
		queue:             queue.New(cfg),
		redactor:          NewRedactor(),
		archiveMsgLimit:   pc.ArchiveMsgLimit,
		maxChunkChars:     pc.MaxChunkSize,
		maxSearchResults:  pc.MaxSearchResults,
		freshnessHalfLife: pc.FreshnessHalfLife,
	}
}

// WithEmbedder sets an embeddings provider for vector search and
// initializes the multi-strategy retriever if not already set.
func (p *Pipeline) WithEmbedder(e embeddings.Provider) *Pipeline {
	p.embedder = e
	if p.retriever == nil {
		p.retriever = NewMultiStrategyRetriever(p.memStore, p.memTree, embeddingAdapter{e}, DefaultWeights(), p.log)
		p.retriever.WithFreshnessHalfLife(p.freshnessHalfLife)
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

// WithProfile sets the L3 persona facet store. When set, the pipeline extracts
// user profile facets from scenario content during L1→L2 aggregation.
func (p *Pipeline) WithProfile(ps *profile.Store) *Pipeline {
	p.profileStore = ps
	return p
}

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

// ForgetAtomsOlderThan removes L1 atoms older than the given duration (the L1
// retention/forgetting mechanism, mirroring TencentDB capture.l0l1RetentionDays).
// Returns the number of atoms deleted. A nil/disabled layered store is a no-op.
func (p *Pipeline) ForgetAtomsOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	if p.layered == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-age)
	return p.layered.DeleteAtomsOlderThan(ctx, cutoff)
}

// AtomDrillDown returns the L0 source refs and L2 scenario for a single atom,
// enabling the full traceability chain: scenario → atom → conversation message.
func (p *Pipeline) AtomDrillDown(ctx context.Context, atomID int64) (*store.Atom, *store.Scenario, error) {
	if p.layered == nil {
		return nil, nil, fmt.Errorf("layered store not available")
	}
	atoms, err := p.layered.ListAtomsByIDs(ctx, []int64{atomID})
	if err != nil {
		return nil, nil, err
	}
	if len(atoms) == 0 {
		return nil, nil, fmt.Errorf("atom %d not found", atomID)
	}
	atom := &atoms[0]
	var scenario *store.Scenario
	if atom.ScenarioID > 0 {
		scenario, err = p.layered.GetScenario(ctx, atom.ScenarioID)
		if err != nil {
			return atom, nil, err
		}
	}
	return atom, scenario, nil
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

	msgs, err := p.convStore.GetMessages(threadID, p.archiveMsgLimit)
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

	// ── L1 extraction: atomize the conversation into atomic facts ──────
	// The layered model (TencentDB L0→L1) extracts self-contained atomic
	// facts from the raw conversation and stores them independently, each
	// traced back to its source message. This is what makes drill-down and
	// per-fact retrieval possible, in contrast to the legacy flat chunk.
	// It reuses the SummarizeMemory key facts when an archivist ran, so no
	// second LLM extraction call is made.
	if p.layered != nil {
		p.extractAtoms(ctx, threadID, doc, archivistResult)
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
	if utf8.RuneCountInString(chunk.Content) > p.maxChunkChars {
		sub := chunkContent(chunk.Content, p.maxChunkChars)
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
		if len(runes) > p.maxChunkChars {
			chunk.Content = string(runes[:p.maxChunkChars]) + "\n... [truncated]"
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

// DefaultSearchLimit returns the configured max-search-results limit, or the
// built-in default when unset.
func (p *Pipeline) DefaultSearchLimit() int {
	if p == nil || p.maxSearchResults <= 0 {
		return defaultMaxSearchResults
	}
	return p.maxSearchResults
}

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

// ApplyRetrievalWeights sets the retriever's explicit numeric weights.
// All-zero weights are ignored (the retriever keeps its current/default set).
func (p *Pipeline) ApplyRetrievalWeights(w RetrievalWeights) {
	if p.retriever == nil {
		return
	}
	if w.FTS5 == 0 && w.Vector == 0 && w.Keyword == 0 && w.Tree == 0 && w.Graph == 0 && w.Episodic == 0 {
		return
	}
	p.retriever.SetWeights(w)
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

// SearchAtoms performs FTS5 full-text search over L1 atomic facts and returns
// the matching atoms, newest first. This is the layered-model retrieval entry:
// it returns the fine-grained facts extracted from conversations rather than
// the legacy flat chunks. A nil/disabled layered store yields nil, nil.
func (p *Pipeline) SearchAtoms(ctx context.Context, query string, limit int) ([]store.Atom, error) {
	if p.layered == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return p.layered.SearchAtoms(ctx, query, limit)
}

// ListRecentAtoms returns the most recent L1 atomic facts. A nil/disabled
// layered store yields nil, nil.
func (p *Pipeline) ListRecentAtoms(ctx context.Context, limit int) ([]store.Atom, error) {
	if p.layered == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return p.layered.ListAtomsRecent(ctx, limit)
}

// SearchAtomsByVector returns the top-k L1 atoms by vector similarity to the
// query (embedded via the pipeline's embedder). A nil/disabled layered store or
// missing embedder yields nil, nil.
func (p *Pipeline) SearchAtomsByVector(ctx context.Context, query string, limit int) ([]store.AtomVectorResult, error) {
	if p.layered == nil || p.embedder == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	vecs, err := p.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, err
	}
	modelSig := fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
	return p.layered.SearchAtomsByVector(ctx, vecs[0], limit, modelSig)
}

// SearchScenariosByVector returns the top-k L2 scenarios by vector similarity
// to the query. A nil/disabled layered store or missing embedder yields nil.
func (p *Pipeline) SearchScenariosByVector(ctx context.Context, query string, limit int) ([]store.ScenarioVectorResult, error) {
	if p.layered == nil || p.embedder == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	vecs, err := p.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, err
	}
	modelSig := fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
	return p.layered.SearchScenariosByVector(ctx, vecs[0], limit, modelSig)
}

func (p *Pipeline) Search(ctx context.Context, query string, limit int) (*SearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Layered L1 atoms: searched independently so fine-grained facts surface
	// alongside (not instead of) the legacy flat results. A nil/disabled
	// layered store yields nil.
	var atoms []store.Atom
	if p.layered != nil {
		if a, err := p.layered.SearchAtoms(ctx, query, limit); err == nil {
			atoms = a
		}
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
		return &SearchResult{Query: query, Chunks: chunks, Scored: scored, Atoms: atoms}, nil
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
	return &SearchResult{Query: query, Chunks: chunks, Nodes: nodes, Atoms: atoms}, nil
}

// SearchResult combines store, tree, and layered-atom results.
type SearchResult struct {
	Query  string
	Chunks []store.MemoryChunk
	Nodes  []*tree.Node
	Atoms  []store.Atom  // layered L1 atomic facts (fine-grained recall)
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
		out := FormatScoredResults(r.Scored)
		if len(r.Atoms) > 0 {
			out += "\n=== Atomic Facts ===\n"
			for _, a := range r.Atoms {
				out += fmt.Sprintf("- %s\n", truncate(a.Content, 200))
			}
		}
		return out
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
	if len(r.Atoms) > 0 {
		out += "=== Atomic Facts ===\n"
		for _, a := range r.Atoms {
			out += fmt.Sprintf("- %s\n", truncate(a.Content, 200))
		}
		out += "\n"
	}
	if len(r.Nodes) > 0 {
		out += "=== Memory Tree ===\n"
		for _, n := range r.Nodes {
			out += fmt.Sprintf("- [%s] %s\n", n.ID, truncate(n.Content, 200))
		}
	}
	if r.TotalResults() == 0 && len(r.Atoms) == 0 {
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

// ── Layered extraction (L1 atoms / L2 scenarios) ────────────────────────

// minAtomsPerScenario is the L1→L2 aggregation threshold: when at least this
// many un-aggregated atoms accumulate, they are rolled into a scenario block.
const minAtomsPerScenario = 8

// extractAtoms atomizes a conversation into L1 atomic facts. It reuses the
// archivist SummarizeMemory key facts when available (no second LLM call);
// otherwise it falls back to heuristic sentence splitting. Each fact is
// redacted before being persisted so PII/secrets never reach the atom table.
func (p *Pipeline) extractAtoms(ctx context.Context, threadID, doc string, archResult *archivist.SummaryResult) {
	// Prefer the archivist's key facts (already computed by SummarizeMemory);
	// fall back to heuristic sentence splitting when none are available.
	facts := archivist.HeuristicFacts(doc)
	if archResult != nil && len(archResult.KeyFacts) > 0 {
		facts = archResult.KeyFacts
	}

	inserted := 0
	for _, f := range facts {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// Redact before persisting: atoms must never carry PII/secrets at rest.
		redacted, _ := p.redactor.Redact(f)
		if redacted == "" {
			continue
		}
		if existing, _ := p.layered.FindAtomByContent(ctx, redacted); existing != nil {
			if archivist.SimpleSimilarity(existing.Content, redacted) >= 0.9 {
				continue // near-duplicate
			}
		}
		atom := store.Atom{
			Content: redacted,
			Source:  "conversation",
			Taint:   store.TaintInternal,
			// L0 traceability is at thread granularity: LLM-rewritten facts
			// cannot be reliably matched back to a single raw message.
			Refs: []store.AtomRef{{ThreadID: threadID}},
		}
		// Embed the atom for vector retrieval when an embedder is present.
		if p.embedder != nil {
			if vecs, e := p.embedder.Embed(ctx, []string{redacted}); e == nil && len(vecs) > 0 {
				atom.Vector = vecs[0]
				atom.EmbeddingModel = fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
			}
		}
		if _, e := p.layered.InsertAtom(ctx, atom); e != nil {
			p.log.Warn("L1 atom insert failed", "thread_id", threadID, "error", e)
			continue
		}
		inserted++
	}

	if inserted > 0 {
		p.aggregateScenarios(ctx)
	}
}

// aggregateScenarios rolls un-aggregated L1 atoms into an L2 scenario block
// once a threshold is reached. The scenario's content is the concatenation of
// its atoms; the atom IDs are persisted for drill-down.
func (p *Pipeline) aggregateScenarios(ctx context.Context) {
	atoms, err := p.layered.ListAtomsUnaggregated(ctx, 1000)
	if err != nil {
		p.log.Warn("L2 aggregation list failed", "error", err)
		return
	}
	if len(atoms) < minAtomsPerScenario {
		return
	}

	// Take the oldest contiguous run of un-aggregated atoms as one scenario.
	batch := atoms[:minAtomsPerScenario]
	ids := make([]int64, 0, len(batch))
	var b strings.Builder
	for _, a := range batch {
		ids = append(ids, a.ID)
		b.WriteString(a.Content)
		b.WriteString("\n")
	}

	sc := store.Scenario{
		Content: strings.TrimSpace(b.String()),
		AtomIDs: ids,
	}
	if p.embedder != nil {
		if vecs, e := p.embedder.Embed(ctx, []string{sc.Content}); e == nil && len(vecs) > 0 {
			sc.Vector = vecs[0]
			sc.EmbeddingModel = fmt.Sprintf("%s:%d", p.embedder.Name(), p.embedder.Dimensions())
		}
	}
	scenarioID, err := p.layered.UpsertScenario(ctx, sc)
	if err != nil {
		p.log.Warn("L2 scenario upsert failed", "error", err)
		return
	}
	if _, err := p.layered.MarkAtomsInScenario(ctx, ids, scenarioID); err != nil {
		p.log.Warn("L2 mark atoms failed", "scenario_id", scenarioID, "error", err)
		return
	}
	// L2→L3: extract user profile facets from the aggregated scenario content.
	if p.profileStore != nil {
		p.extractPersona(ctx, scenarioID, batch)
	}
	p.log.Info("aggregated L1 atoms into scenario", "scenario_id", scenarioID, "atoms", len(ids))
}

// extractPersona derives L3 user-profile facets from a scenario's atoms using
// lightweight pattern heuristics (matching ArchivistHook.extractSimpleFacets).
// Each matched facet is upserted with the scenario ID as its evidence source.
func (p *Pipeline) extractPersona(ctx context.Context, scenarioID int64, atoms []store.Atom) {
	now := float64(time.Now().UnixNano()) / 1e9
	for _, a := range atoms {
		msg := strings.ToLower(a.Content)
		type check struct {
			pattern   string
			facetType profile.FacetType
			key       string
		}
		checks := []check{
			{"i work at ", profile.FacetRole, "employer"},
			{"i am a ", profile.FacetRole, "job_title"},
			{"i'm a ", profile.FacetRole, "job_title"},
			{"my name is ", profile.FacetContext, "name"},
			{"i live in ", profile.FacetContext, "location"},
			{"i prefer ", profile.FacetPreference, "general_preference"},
			{"i use ", profile.FacetSkill, "tool"},
		}
		for _, c := range checks {
			if idx := strings.Index(msg, c.pattern); idx >= 0 {
				val := extractAfter(msg[idx+len(c.pattern):])
				if val != "" && len(val) < 200 {
					_ = p.profileStore.UpsertFacet(&profile.ProfileFacet{
						FacetType:        c.facetType,
						Key:              c.key,
						Value:            val,
						Confidence:       0.5,
						SourceSegmentIDs: fmt.Sprintf("%d", scenarioID),
						LastSeenAt:       now,
					})
				}
				break
			}
		}
	}
}

// extractAfter returns text up to the first punctuation or 80 chars.
func extractAfter(s string) string {
	for i, r := range s {
		if r == '.' || r == ',' || r == '!' || r == '?' || r == '\n' || i > 80 {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
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
