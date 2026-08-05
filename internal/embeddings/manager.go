package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Cache caches embeddings to avoid redundant API calls. Safe for concurrent use.
type Cache struct {
	mu      sync.RWMutex
	entries map[string][]float32
}

func NewCache() *Cache {
	return &Cache{entries: make(map[string][]float32)}
}

func (c *Cache) Get(text string) ([]float32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.entries[text]
	return v, ok
}

func (c *Cache) Set(text string, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[text] = vec
}

// Manager provides a unified embedding interface with caching and fallback.
type Manager struct {
	log      *slog.Logger
	provider Provider
	cache    *Cache
}

// NewManager creates a new embedding manager.
func NewManager(log *slog.Logger, provider Provider) *Manager {
	return &Manager{
		log:      log,
		provider: provider,
		cache:    NewCache(),
	}
}

// Embed generates embeddings for the given texts, using cache where available.
func (m *Manager) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("no embedding provider configured")
	}

	// Check cache first
	var uncached []string
	uncachedIdx := make([]int, 0)
	for i, t := range texts {
		if _, ok := m.cache.Get(t); !ok {
			uncached = append(uncached, t)
			uncachedIdx = append(uncachedIdx, i)
		}
	}

	if len(uncached) > 0 {
		vecs, err := m.provider.Embed(ctx, uncached)
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
		for i, idx := range uncachedIdx {
			m.cache.Set(texts[idx], vecs[i])
		}
	}

	// Build result from cache
	result := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := m.cache.Get(t)
		result[i] = v
	}
	return result, nil
}

// EmbedSingle is a convenience wrapper for embedding a single text.
func (m *Manager) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	vecs, err := m.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return vecs[0], nil
}

// Dimensions returns the dimensionality of the embedding provider.
func (m *Manager) Dimensions() int {
	if m.provider == nil {
		return 0
	}
	return m.provider.Dimensions()
}

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Simple Newton's method for float32 sqrt
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}
