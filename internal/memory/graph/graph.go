package graph

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/simon/mneme/internal/memory/store"
)

// Edge represents a weighted co-occurrence relationship between two entities.
type Edge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
	Count  int     `json:"count"`
}

const maxInMemoryEdges = 100000

// Graph is an in-memory co-occurrence graph persisted to SQLite.
// It tracks which entities appear together in conversations and derives
// weighted edges for knowledge-graph traversal during memory recall.
type Graph struct {
	mu    sync.RWMutex
	edges map[string]*Edge // keyed by "source::target" (sorted alphabetically)
	db    *sql.DB
}

// New creates a new Graph, optionally backed by a SQLite database.
func New(db *sql.DB) (*Graph, error) {
	g := &Graph{
		edges: make(map[string]*Edge),
		db:    db,
	}
	if db != nil {
		if err := g.migrate(); err != nil {
			return nil, err
		}
		if err := g.load(); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func (g *Graph) migrate() error {
	_, err := g.db.Exec(`
		CREATE TABLE IF NOT EXISTS entity_edges (
			source TEXT NOT NULL,
			target TEXT NOT NULL,
			weight REAL NOT NULL DEFAULT 0,
			count INTEGER NOT NULL DEFAULT 0,
				relation TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (source, target)
		);
		CREATE INDEX IF NOT EXISTS idx_entity_edges_source ON entity_edges(source);
		CREATE INDEX IF NOT EXISTS idx_entity_edges_target ON entity_edges(target);
	`)
	return err
}

func (g *Graph) load() error {
	rows, err := g.db.Query("SELECT source, target, weight, count FROM entity_edges")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.Source, &e.Target, &e.Weight, &e.Count); err != nil {
			return err

			// Migration: add relation column for existing databases.
		}
		key := edgeKey(e.Source, e.Target)
		// Take a copy for the map
		edgeCopy := e
		g.edges[key] = &edgeCopy
	}
	return rows.Err()
}

// RecordCoOccurrenceWithEmbeddings records co-occurrence and computes
// embedding-based similarity for edge pairs in a single lock-held operation.
// When embeddings are provided, edge weights blend co-occurrence frequency (70%)
// with cosine similarity of entity embeddings (30%).
func (g *Graph) RecordCoOccurrenceWithEmbeddings(entities []string, embeddings map[string][]float32) {
	if len(entities) < 2 {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Evict lowest-weight edges if we've exceeded the in-memory cap.
	if len(g.edges) >= maxInMemoryEdges {
		g.evictLocked(len(g.edges) - maxInMemoryEdges + 1)
	}

	deduped := dedupeEntities(entities)
	for i := 0; i < len(deduped); i++ {
		for j := i + 1; j < len(deduped); j++ {
			key := edgeKey(deduped[i], deduped[j])
			if existing, ok := g.edges[key]; ok {
				existing.Count++
				// Blend co-occurrence weight with embedding similarity when available.
				baseWeight := computeWeight(existing.Count)
				if embeddings != nil {
					embA := embeddings[deduped[i]]
					embB := embeddings[deduped[j]]
					if len(embA) > 0 && len(embB) > 0 {
						sim := cosineSimilarity(embA, embB)
						existing.Weight = 0.7*baseWeight + 0.3*sim
						continue
					}
				}
				existing.Weight = baseWeight
			} else {
				g.edges[key] = &Edge{
					Source: deduped[i],
					Target: deduped[j],
					Count:  1,
					Weight: computeWeight(1),
				}
			}
		}
	}
}

// RecordCoOccurrence records that a set of entities appeared together in the same context.
// Increments the edge weight for each pair of entities.
func (g *Graph) RecordCoOccurrence(entities []string) {
	if len(entities) < 2 {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Evict lowest-weight edges if we've exceeded the in-memory cap.
	if len(g.edges) >= maxInMemoryEdges {
		g.evictLocked(len(g.edges) - maxInMemoryEdges + 1)
	}

	deduped := dedupeEntities(entities)
	for i := 0; i < len(deduped); i++ {
		for j := i + 1; j < len(deduped); j++ {
			key := edgeKey(deduped[i], deduped[j])
			if existing, ok := g.edges[key]; ok {
				existing.Count++
				existing.Weight = computeWeight(existing.Count)
			} else {
				g.edges[key] = &Edge{
					Source: deduped[i],
					Target: deduped[j],
					Count:  1,
					Weight: computeWeight(1),
				}
			}
		}
	}
}

// evictLocked removes the n lowest-weight edges. Must be called with mu held.
func (g *Graph) evictLocked(n int) {
	type entry struct {
		key    string
		weight float64
	}
	entries := make([]entry, 0, len(g.edges))
	for k, e := range g.edges {
		entries = append(entries, entry{key: k, weight: e.Weight})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].weight < entries[j].weight
	})
	for i := 0; i < n && i < len(entries); i++ {
		delete(g.edges, entries[i].key)
	}
}

// RecordCoOccurrencePersisted records and persists to the database.
func (g *Graph) RecordCoOccurrencePersisted(entities []string) {
	g.RecordCoOccurrence(entities)
	if g.db == nil {
		return
	}
	for i := 0; i < len(entities); i++ {
		for j := i + 1; j < len(entities); j++ {
			key := edgeKey(entities[i], entities[j])
			g.mu.RLock()
			e, ok := g.edges[key]
			g.mu.RUnlock()
			if !ok {
				continue
			}
			if _, err := g.db.Exec(
				`INSERT INTO entity_edges (source, target, weight, count) VALUES (?, ?, ?, ?)
				 ON CONFLICT(source, target) DO UPDATE SET weight=excluded.weight, count=excluded.count`,
				e.Source, e.Target, e.Weight, e.Count,
			); err != nil {
				slog.Warn("graph: failed to persist edge", "source", e.Source, "target", e.Target, "error", err)
			}
		}
	}
}

// buildAdjacencyLocked constructs a target→[]Edge index keyed by lowercased
// entity name. The caller must hold at least a read lock. This index avoids
// scanning the full edge list in GetRelated's multi-hop traversal.
func (g *Graph) buildAdjacencyLocked() map[string][]Edge {
	adj := make(map[string][]Edge, len(g.edges)*2)
	for _, e := range g.edges {
		sl := strings.ToLower(e.Source)
		tl := strings.ToLower(e.Target)
		adj[sl] = append(adj[sl], *e)
		adj[tl] = append(adj[tl], *e)
	}
	return adj
}

// GetRelated returns entities related to the given entity, sorted by weight descending.
// traversalDepth controls how many hops to follow (1 = direct edges only).
func (g *Graph) GetRelated(entity string, limit int, traversalDepth int) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if traversalDepth <= 0 {
		traversalDepth = 1
	}

	// Build adjacency index once to avoid O(N²) scanning in multi-hop.
	adj := g.buildAdjacencyLocked()

	// Single-hop: collect all edges involving the entity.
	candidates := make(map[string]float64)
	normalized := strings.ToLower(entity)

	for _, e := range adj[normalized] {
		other := e.Target
		if strings.ToLower(other) == normalized {
			other = e.Source
		}
		candidates[other] = e.Weight
	}

	// Multi-hop: for each direct neighbor, find THEIR neighbors with decay.
	if traversalDepth > 1 {
		decay := 0.5
		firstHop := make(map[string]float64)
		for k, v := range candidates {
			firstHop[k] = v
		}
		for neighbor, baseWeight := range firstHop {
			neighborNorm := strings.ToLower(neighbor)
			for _, e := range adj[neighborNorm] {
				other := e.Target
				otherNorm := strings.ToLower(other)
				if otherNorm == neighborNorm {
					other = e.Source
					otherNorm = strings.ToLower(other)
				}
				if otherNorm == normalized {
					continue
				}
				decayedWeight := baseWeight * e.Weight * decay
				if existing, ok := candidates[other]; !ok || decayedWeight > existing {
					candidates[other] = decayedWeight
				}
			}
		}
	}

	// Convert to sorted slice
	results := make([]Edge, 0, len(candidates))
	for target, weight := range candidates {
		results = append(results, Edge{
			Source: entity,
			Target: target,
			Weight: weight,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Weight > results[j].Weight
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

// GetAllEdges returns all edges in the graph.
func (g *Graph) GetAllEdges() []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	results := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		results = append(results, *e)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Weight > results[j].Weight
	})
	return results
}

// Stats returns summary statistics.
func (g *Graph) Stats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return map[string]interface{}{
		"edges": len(g.edges),
		"nodes": g.nodeCount(),
	}
}

func (g *Graph) nodeCount() int {
	nodes := make(map[string]bool)
	for _, e := range g.edges {
		nodes[e.Source] = true
		nodes[e.Target] = true
	}
	return len(nodes)
}

// computeWeight uses logarithmic decay so each additional co-occurrence adds diminishing weight.
// This prevents frequently-mentioned entity pairs from completely dominating.
func computeWeight(count int) float64 {
	if count <= 0 {
		return 0
	}
	return 1.0 + math.Log2(float64(count))
}

func edgeKey(a, b string) string {
	if strings.ToLower(a) < strings.ToLower(b) {
		return strings.ToLower(a) + "::" + strings.ToLower(b)
	}
	return strings.ToLower(b) + "::" + strings.ToLower(a)
}

// cosineSimilarity delegates to store.CosineSimilarity, which clamps
// results to [0, 1] for semantically meaningful scores.
func cosineSimilarity(a, b []float32) float64 {
	return store.CosineSimilarity(a, b)
}

func dedupeEntities(entities []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(entities))
	for _, e := range entities {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		lower := strings.ToLower(e)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, e)
		}
	}
	return result
}

// FormatRelated returns a human-readable summary of related entities.
func FormatRelated(entity string, edges []Edge) string {
	if len(edges) == 0 {
		return fmt.Sprintf("No related entities found for %q.", entity)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Entities related to %q:\n", entity))
	for _, e := range edges {
		b.WriteString(fmt.Sprintf("  - %s (weight: %.2f, co-occurrences: %d)\n", e.Target, e.Weight, e.Count))
	}
	return b.String()
}
