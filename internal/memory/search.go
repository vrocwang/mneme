package memory

import (

	"github.com/simon/mneme/internal/memory/store"
)

// MemorySearchOps provides agent-facing memory retrieval operations
// that wrap the Pipeline with MMR reranking and chunk context.
type MemorySearchOps struct {
	pipeline *Pipeline
}// ── MMR (Maximum Marginal Relevance) reranking ─────────────────────────
// ── Helpers ────────────────────────────────────────────────────────────

// cosineSimilarity delegates to store.CosineSimilarity, which includes
// clamping to [0, 1] for semantically meaningful scores.
func cosineSimilarity(a, b []float32) float64 {
	return store.CosineSimilarity(a, b)
}
