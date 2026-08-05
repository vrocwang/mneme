package memory

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/simon/mneme/internal/memory/store"
)

// Checkpoint captures a memory snapshot at a point in time.
type Checkpoint struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	ChunkIDs  []int64   `json:"chunk_ids"`
	HashSum   string    `json:"hash_sum"` // SHA-256 of sorted chunk ID list
}

// DiffResult describes changes between two memory checkpoints.
type DiffResult struct {
	CheckpointA Checkpoint `json:"checkpoint_a"`
	CheckpointB Checkpoint `json:"checkpoint_b"`

	Added   []int64 `json:"added"`   // chunk IDs present in B but not A
	Removed []int64 `json:"removed"` // chunk IDs present in A but not B
	Changed []int64 `json:"changed"` // chunk IDs in both but content differs

	SourceBreakdown map[string]DiffCount `json:"source_breakdown"` // per-source counts
}

// CrossSourceDiff compares chunks added between two checkpoints grouped
// by data source, showing what each integration contributed.
type CrossSourceDiff struct {
	BySource   map[string][]store.MemoryChunk `json:"by_source"`
	TotalAdded int                            `json:"total_added"`
}

// DiffCount tracks add/remove/change counts per source.
type DiffCount struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}

// MemoryDiff provides snapshot and diff operations over memory.
type MemoryDiff struct {
	store *store.Store
}

// NewMemoryDiff creates a new MemoryDiff service.
func NewMemoryDiff(s *store.Store) *MemoryDiff {
	return &MemoryDiff{store: s}
}

// CreateCheckpoint captures the current set of chunk IDs as a snapshot.
func (d *MemoryDiff) CreateCheckpoint(label string) (*Checkpoint, error) {
	chunks, err := d.store.ListRecent(100000)
	if err != nil {
		return nil, fmt.Errorf("create checkpoint: list chunks: %w", err)
	}

	ids := make([]int64, len(chunks))
	for i, c := range chunks {
		ids[i] = c.ID
	}

	h := sha256.New()
	for _, id := range ids {
		h.Write([]byte(fmt.Sprintf("%d,", id)))
	}
	hashSum := fmt.Sprintf("%x", h.Sum(nil))

	cp := &Checkpoint{
		ID:        fmt.Sprintf("cp_%d", time.Now().UnixMilli()),
		Label:     label,
		CreatedAt: time.Now().UTC(),
		ChunkIDs:  ids,
		HashSum:   hashSum,
	}
	return cp, nil
}

// Diff computes the difference between two checkpoints.
func (d *MemoryDiff) Diff(a, b *Checkpoint) (*DiffResult, error) {
	aSet := toSet(a.ChunkIDs)
	bSet := toSet(b.ChunkIDs)

	result := &DiffResult{
		CheckpointA:     *a,
		CheckpointB:     *b,
		SourceBreakdown: make(map[string]DiffCount),
	}

	// Find added and removed IDs.
	for id := range bSet {
		if !aSet[id] {
			result.Added = append(result.Added, id)
		}
	}
	for id := range aSet {
		if !bSet[id] {
			result.Removed = append(result.Removed, id)
		}
	}
	// IDs in both are potentially changed.
	for id := range aSet {
		if bSet[id] {
			result.Changed = append(result.Changed, id)
		}
	}

	return result, nil
}

// DiffWithContent computes the diff and fetches actual chunk content for
// cross-source breakdown.
func (d *MemoryDiff) DiffWithContent(a, b *Checkpoint) (*CrossSourceDiff, error) {
	result := &CrossSourceDiff{
		BySource: make(map[string][]store.MemoryChunk),
	}

	for _, id := range b.ChunkIDs {
		if !containsID(a.ChunkIDs, id) {
			// This chunk was added since checkpoint A.
			// Fetch it from the store.
			chunks, _ := d.store.Search(fmt.Sprintf("id:%d", id), 1)
			if len(chunks) > 0 {
				src := chunks[0].Source
				result.BySource[src] = append(result.BySource[src], chunks[0])
				result.TotalAdded++
			}
		}
	}
	return result, nil
}

// ── Helpers ────────────────────────────────────────────────────────────

func toSet(ids []int64) map[int64]bool {
	s := make(map[int64]bool, len(ids))
	for _, id := range ids {
		s[id] = true
	}
	return s
}

func containsID(ids []int64, target int64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// FormatDiffReport formats a DiffResult as a human-readable markdown string.
func FormatDiffReport(diff *DiffResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Memory Diff: %s → %s\n\n", diff.CheckpointA.Label, diff.CheckpointB.Label))
	b.WriteString(fmt.Sprintf("- Checkpoint A: %s (%d chunks)\n", diff.CheckpointA.CreatedAt.Format(time.RFC3339), len(diff.CheckpointA.ChunkIDs)))
	b.WriteString(fmt.Sprintf("- Checkpoint B: %s (%d chunks)\n\n", diff.CheckpointB.CreatedAt.Format(time.RFC3339), len(diff.CheckpointB.ChunkIDs)))

	b.WriteString("| Change | Count |\n|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Added   | %d |\n", len(diff.Added)))
	b.WriteString(fmt.Sprintf("| Removed | %d |\n", len(diff.Removed)))
	b.WriteString(fmt.Sprintf("| Changed | %d |\n", len(diff.Changed)))

	if len(diff.SourceBreakdown) > 0 {
		b.WriteString("\n### By Source\n\n")
		b.WriteString("| Source | Added | Removed | Changed |\n|--------|-------|---------|--------|\n")
		for src, counts := range diff.SourceBreakdown {
			b.WriteString(fmt.Sprintf("| %s | %d | %d | %d |\n", src, counts.Added, counts.Removed, counts.Changed))
		}
	}

	return b.String()
}
