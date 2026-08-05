package memory

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/simon/mneme/internal/memory/store"
	"github.com/simon/mneme/internal/memory/tree"
)

type RetrievalWeights struct {
	FTS5     float64
	Vector   float64
	Keyword  float64
	Tree     float64
	Graph    float64
	Episodic float64
}

func DefaultWeights() RetrievalWeights {
	return RetrievalWeights{FTS5: 0.30, Vector: 0.25, Keyword: 0.08, Tree: 0.05, Graph: 0.22, Episodic: 0.10}
}

// WeightProfile is a named retrieval weight configuration.
type WeightProfile string

const (
	ProfileBalanced   WeightProfile = "balanced"
	ProfileSemantic   WeightProfile = "semantic"
	ProfileLexical    WeightProfile = "lexical"
	ProfileGraphFirst WeightProfile = "graph_first"
)

// ProfileWeights returns the retrieval weights for a named profile.
func ProfileWeights(p WeightProfile) RetrievalWeights {
	switch p {
	case ProfileSemantic:
		return RetrievalWeights{FTS5: 0.15, Vector: 0.65, Keyword: 0.10, Tree: 0.05, Graph: 0.05, Episodic: 0.0}
	case ProfileLexical:
		return RetrievalWeights{FTS5: 0.30, Vector: 0.10, Keyword: 0.50, Tree: 0.05, Graph: 0.05, Episodic: 0.0}
	case ProfileGraphFirst:
		return RetrievalWeights{FTS5: 0.10, Vector: 0.20, Keyword: 0.05, Tree: 0.05, Graph: 0.55, Episodic: 0.05}
	default:
		return DefaultWeights()
	}
}

type ScoredChunk struct {
	Chunk   store.MemoryChunk
	Score   float64
	Signals map[string]float64
}

type MultiStrategyRetriever struct {
	mu             sync.Mutex
	store          *store.Store
	memTree        *tree.Tree
	embedder       embeddingProvider
	graphScorer    GraphScorer
	episodicScorer EpisodicScorer
	weights        RetrievalWeights
	log            *slog.Logger
}

type embeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Name() string
	Dimensions() int
}

type GraphScorer interface {
	GraphScore(query string, chunkContent string, chunkVector []float32) float64
}

type EpisodicScorer interface {
	EpisodicScore(query string, limit int) float64
}

func NewMultiStrategyRetriever(s *store.Store, t *tree.Tree, emb embeddingProvider, w RetrievalWeights, log *slog.Logger) *MultiStrategyRetriever {
	w = normalizeWeights(w)
	if log == nil {
		log = slog.Default()
	}
	return &MultiStrategyRetriever{store: s, memTree: t, embedder: emb, weights: w, log: log}
}

func (r *MultiStrategyRetriever) WithGraphScorer(gs GraphScorer) *MultiStrategyRetriever {
	r.graphScorer = gs
	return r
}
func (r *MultiStrategyRetriever) WithEpisodicScorer(es EpisodicScorer) *MultiStrategyRetriever {
	r.episodicScorer = es
	return r
}

// SetWeights applies new retrieval weights.
func (r *MultiStrategyRetriever) SetWeights(w RetrievalWeights) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.weights = normalizeWeights(w)
}

// ApplyProfile sets retrieval weights from a named profile.
func (r *MultiStrategyRetriever) ApplyProfile(p WeightProfile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.weights = ProfileWeights(p)
	r.weights = normalizeWeights(r.weights)
}

// Weights returns the current retrieval weights (thread-safe).
func (r *MultiStrategyRetriever) Weights() RetrievalWeights {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.weights
}

func normalizeWeights(w RetrievalWeights) RetrievalWeights {
	sum := w.FTS5 + w.Vector + w.Keyword + w.Tree + w.Graph + w.Episodic
	if sum == 0 {
		return DefaultWeights()
	}
	w.FTS5 /= sum
	w.Vector /= sum
	w.Keyword /= sum
	w.Tree /= sum
	w.Graph /= sum
	w.Episodic /= sum
	return w
}

func (r *MultiStrategyRetriever) Search(ctx context.Context, query string, limit int) ([]ScoredChunk, error) {
	r.mu.Lock()
	w := r.weights
	r.mu.Unlock()
	return r.searchWeighted(ctx, query, limit, w)
}

// SearchWeighted is like Search but uses the caller-supplied weights
// instead of the instance's current weights. The instance is not mutated.
// This is safe for concurrent use — no lock is held for the duration.
func (r *MultiStrategyRetriever) SearchWeighted(ctx context.Context, query string, limit int, w RetrievalWeights) ([]ScoredChunk, error) {
	w = normalizeWeights(w)
	return r.searchWeighted(ctx, query, limit, w)
}

func (r *MultiStrategyRetriever) searchWeighted(ctx context.Context, query string, limit int, w RetrievalWeights) ([]ScoredChunk, error) {
	if limit <= 0 {
		limit = 10
	}

	results := make(map[int64]*scoredEntry)

	if w.FTS5 > 0 {
		ftsChunks, err := r.store.Search(query, limit*2)
		if err != nil {
			r.log.Warn("fts5 search failed in retrieval", "error", err)
		}
		for i, c := range ftsChunks {
			score := w.FTS5 * (1.0 - float64(i)/float64(len(ftsChunks)+1))
			r.accumulate(results, c, score, "fts5")
		}
	}
	if w.Vector > 0 && r.embedder != nil {
		vecs, err := r.embedder.Embed(ctx, []string{query})
		if err == nil && len(vecs) > 0 {
			modelSig := fmt.Sprintf("%s:%d", r.embedder.Name(), r.embedder.Dimensions())
			vecResults, verr := r.store.SearchByVector(vecs[0], limit*2, modelSig)
			if verr != nil {
				r.log.Warn("vector search failed in retrieval", "error", verr)
			}
			maxVec := 0.0
			for _, vr := range vecResults {
				if vr.Similarity > maxVec {
					maxVec = vr.Similarity
				}
			}
			for _, vr := range vecResults {
				score := w.Vector * (vr.Similarity / maxF(maxVec, 0.001))
				r.accumulate(results, vr.Chunk, score, "vector")
			}
		}
	}
	if w.Keyword > 0 {
		allRecent, err := r.store.ListRecent(limit * 3)
		if err != nil {
			r.log.Warn("keyword recent list failed in retrieval", "error", err)
		}
		queryTokens := tokenize(query)
		for _, c := range allRecent {
			overlap := keywordOverlap(queryTokens, c.Content)
			if overlap > 0 {
				r.accumulate(results, c, w.Keyword*overlap, "keyword")
			}
		}
	}
	if w.Tree > 0 && r.memTree != nil {
		treeNodes := r.memTree.Search(query, limit*2)
		for i, n := range treeNodes {
			if n != nil {
				score := w.Tree * (1.0 - float64(i)/float64(len(treeNodes)+1))
				chunk := store.MemoryChunk{Source: "tree", Content: n.Content, Summary: n.ID}
				r.accumulate(results, chunk, score, "tree")
			}
		}
	}
	if w.Graph > 0 && r.graphScorer != nil {
		allRecent, err := r.store.ListRecent(limit * 3)
		if err != nil {
			r.log.Warn("graph recent list failed in retrieval", "error", err)
		}
		for _, c := range allRecent {
			gs := r.graphScorer.GraphScore(query, c.Content, c.Vector)
			if gs > 0 {
				r.accumulate(results, c, w.Graph*gs, "graph")
			}
		}
	}
	if w.Episodic > 0 && r.episodicScorer != nil {
		es := r.episodicScorer.EpisodicScore(query, limit)
		if es > 0 {
			allRecent, err := r.store.ListRecent(limit * 2)
			if err != nil {
				r.log.Warn("episodic recent list failed in retrieval", "error", err)
			}
			queryTokens := tokenize(query)
			for _, c := range allRecent {
				// Only boost chunks with content overlap, not every
				// chunk equally. A global constant boost is meaningless
				// for ranking — the episodic signal must discriminate.
				overlap := keywordOverlap(queryTokens, c.Content)
				if overlap > 0 {
					r.accumulate(results, c, w.Episodic*es*overlap*0.1, "episodic")
				}
			}
		}
	}

	ranked := make([]ScoredChunk, 0, len(results))
	for _, entry := range results {
		ranked = append(ranked, ScoredChunk{Chunk: entry.chunk, Score: entry.score, Signals: entry.signals})
	}
	// Apply temporal boost for time-sensitive queries.
	for i := range ranked {
		boost := temporalBoost(query, ranked[i].Chunk.CreatedAt)
		if boost != 1.0 {
			ranked[i].Score *= boost
			ranked[i].Signals["temporal"] = boost
		}
	}

	// Apply freshness decay: older chunks naturally fade.
	for i := range ranked {
		decay := freshnessDecay(ranked[i].Chunk.CreatedAt)
		ranked[i].Score *= decay
		ranked[i].Signals["freshness"] = decay
	}

	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

type scoredEntry struct {
	chunk   store.MemoryChunk
	score   float64
	signals map[string]float64
}

func (r *MultiStrategyRetriever) accumulate(seen map[int64]*scoredEntry, c store.MemoryChunk, score float64, signal string) {
	key := c.ID
	if key == 0 {
		key = int64(hashString64(c.Content))
	}
	if entry, ok := seen[key]; ok {
		entry.score += score
		entry.signals[signal] += score
	} else {
		seen[key] = &scoredEntry{chunk: c, score: score, signals: map[string]float64{signal: score}}
	}
}

func tokenize(s string) []string {
	var tokens []string
	for _, w := range strings.Fields(strings.ToLower(s)) {
		// Strip leading/trailing punctuation but preserve internal punctuation
		// (e.g. "doesn't" stays intact instead of splitting into "doesn" and "t").
		w = strings.Trim(w, ".,;:!?\"'()[]{}")
		if len(w) > 1 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

func keywordOverlap(queryTokens []string, content string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}
	lower := strings.ToLower(content)
	matches := 0
	for _, t := range queryTokens {
		if strings.Contains(lower, t) {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
}

func hashString64(s string) uint64 {
	// FNV-1a 64-bit hash — 64-bit space avoids collisions in large stores.
	const fnvPrime = 1099511628211
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime
	}
	return h
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func FormatScoredResults(results []ScoredChunk) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. [%.2f] %s\n   %s\n", i+1, r.Score, truncateStr(r.Chunk.Summary, 80), truncateStr(r.Chunk.Content, 200)))
		if i >= 9 {
			break
		}
	}
	if len(results) == 0 {
		b.WriteString("No results found.\n")
	}
	return b.String()
}
func truncateStr(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	// Truncate to a valid UTF-8 boundary to avoid splitting multi-byte runes.
	if idx := max; idx > 0 {
		for idx > 0 && !utf8.RuneStart(s[idx]) {
			idx--
		}
		return s[:idx] + "..."
	}
	return s[:max] + "..."
}

// temporalBoost returns a boost factor based on time expressions in the query.
// Supports both English and Chinese time expressions.
func temporalBoost(query string, createdAt string) float64 {
	lower := strings.ToLower(query)
	days := 0
	switch {
	// English
	case strings.Contains(lower, "last week") || strings.Contains(lower, "past week"):
		days = 7
	case strings.Contains(lower, "yesterday"):
		days = 1
	case strings.Contains(lower, "today"):
		days = 1
	case strings.Contains(lower, "last month") || strings.Contains(lower, "past month"):
		days = 30
	case strings.Contains(lower, "recently") || strings.Contains(lower, "latest"):
		days = 3
	// Chinese
	case strings.Contains(query, "上周") || strings.Contains(query, "上星期") ||
		strings.Contains(query, "过去一周") || strings.Contains(query, "这周") ||
		strings.Contains(query, "本周"):
		days = 7
	case strings.Contains(query, "昨天") || strings.Contains(query, "昨日"):
		days = 1
	case strings.Contains(query, "今天") || strings.Contains(query, "今日"):
		days = 1
	case strings.Contains(query, "上个月") || strings.Contains(query, "上月") ||
		strings.Contains(query, "过去一个月"):
		days = 30
	case strings.Contains(query, "最近") || strings.Contains(query, "近期") ||
		strings.Contains(query, "最新"):
		days = 3
	// Additional Chinese
	case strings.Contains(query, "去年") || strings.Contains(query, "过去一年"):
		days = 365
	case strings.Contains(query, "今年"):
		days = 365
	default:
		return 1.0 // no temporal boost
	}
	if createdAt == "" || days == 0 {
		return 1.0
	}
	// Parse the created_at timestamp and check if within window.
	t, err := timeParse(createdAt)
	if err != nil {
		return 1.0
	}
	if time.Since(t).Hours() < float64(days*24) {
		return 1.5
	}
	return 1.0
}

// freshnessDecay returns a decay factor based on the age of the content.
// Uses exponential decay: exp(-ageInHours / halfLifeHours).
// Default half-life is 7 days (168 hours).
func freshnessDecay(createdAt string) float64 {
	if createdAt == "" {
		return 1.0
	}
	t, err := timeParse(createdAt)
	if err != nil {
		return 1.0
	}
	ageHours := time.Since(t).Hours()
	if ageHours < 0 {
		return 1.0
	}
	halfLife := 168.0 // 7 days
	return math.Exp(-ageHours / halfLife)
}

func timeParse(s string) (time.Time, error) {
	// Try multiple common formats.
	formats := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05-07:00",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
}
