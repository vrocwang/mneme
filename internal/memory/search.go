package memory

import (
	"context"
	"math"

	"github.com/simon/mneme/internal/memory/store"
)

// MemorySearchOps provides agent-facing memory retrieval operations
// that wrap the Pipeline with MMR reranking and chunk context.
type MemorySearchOps struct {
	pipeline *Pipeline
}

// NewMemorySearchOps creates a new MemorySearchOps.
func NewMemorySearchOps(p *Pipeline) *MemorySearchOps {
	return &MemorySearchOps{pipeline: p}
}

// SearchWithMMR performs hybrid search with MMR diversity reranking.
// When an embedder is available, the query is embedded and used as the
// relevance signal for MMR; otherwise falls back to pre-computed scores.
func (m *MemorySearchOps) SearchWithMMR(ctx context.Context, query string, limit int, lambda float64) (*SearchResult, error) {
	result, err := m.pipeline.Search(ctx, query, limit*2)
	if err != nil {
		return nil, err
	}

	if len(result.Scored) == 0 {
		return result, nil
	}

	// Convert ScoredChunks to VectorResult for MMR.
	candidates := make([]store.VectorResult, len(result.Scored))
	for i, sc := range result.Scored {
		candidates[i] = store.VectorResult{
			Chunk:      sc.Chunk,
			Similarity: sc.Score,
		}
	}

	// Compute query embedding for query-aware MMR relevance when available.
	var queryVec []float32
	if m.pipeline.embedder != nil {
		vecs, embErr := m.pipeline.embedder.Embed(ctx, []string{query})
		if embErr == nil && len(vecs) > 0 {
			queryVec = vecs[0]
		}
	}

	reranked := MMRRerank(queryVec, candidates, lambda, limit)

	result.Scored = make([]ScoredChunk, len(reranked))
	for i, rr := range reranked {
		result.Scored[i] = ScoredChunk{
			Chunk: rr.Chunk,
			Score: rr.Similarity,
		}
	}
	return result, nil
}

// ── MMR (Maximum Marginal Relevance) reranking ─────────────────────────

// MMRRerank applies Maximum Marginal Relevance diversity reranking.
// When queryVec is provided, relevance is computed as cosine similarity
// between the query vector and each candidate's embedding. When nil,
// relevance falls back to the candidate's pre-computed Similarity score.
// lambda balances relevance vs. redundancy. Default lambda = 0.7.
func MMRRerank(queryVec []float32, candidates []store.VectorResult, lambda float64, topK int) []store.VectorResult {
	if lambda <= 0 {
		lambda = 0.7
	}
	if len(candidates) <= topK {
		return candidates
	}

	// Pre-compute query-aware relevance scores when a query vector is available.
	hasQueryVec := len(queryVec) > 0
	queryRelevance := make([]float64, len(candidates))
	for i, c := range candidates {
		if hasQueryVec {
			queryRelevance[i] = cosineSimilarity(queryVec, c.Chunk.Vector)
		} else {
			queryRelevance[i] = c.Similarity
		}
	}

	selected := make([]store.VectorResult, 0, topK)
	remaining := make([]store.VectorResult, len(candidates))
	copy(remaining, candidates)
	remainingRelevance := make([]float64, len(queryRelevance))
	copy(remainingRelevance, queryRelevance)

	for len(selected) < topK && len(remaining) > 0 {
		bestIdx := 0
		bestScore := math.Inf(-1)

		for i, c := range remaining {
			relevance := remainingRelevance[i]
			redundancy := 0.0
			for _, s := range selected {
				red := cosineSimilarity(c.Chunk.Vector, s.Chunk.Vector)
				if red > redundancy {
					redundancy = red
				}
			}
			mmr := lambda*relevance - (1.0-lambda)*redundancy
			if mmr > bestScore {
				bestScore = mmr
				bestIdx = i
			}
		}

		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
		remainingRelevance = append(remainingRelevance[:bestIdx], remainingRelevance[bestIdx+1:]...)
	}

	return selected
}

// ── Helpers ────────────────────────────────────────────────────────────

// cosineSimilarity delegates to store.CosineSimilarity, which includes
// clamping to [0, 1] for semantically meaningful scores.
func cosineSimilarity(a, b []float32) float64 {
	return store.CosineSimilarity(a, b)
}
