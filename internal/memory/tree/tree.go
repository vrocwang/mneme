package tree

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Node is a node in the summary tree.
type Node struct {
	ID        string
	ParentID  string
	Content   string
	Summary   string
	Children  []*Node
	Count     int // number of items bucketed into this node
	CreatedAt time.Time
	SealedAt  *time.Time
}

// Tree holds the memory summary tree with optional SQLite persistence.
type Tree struct {
	mu         sync.RWMutex
	root       *Node
	nodeMap    map[string]*Node
	bucketSize int
	db         *sql.DB // optional persistence
}

func NewTree(bucketSize int) *Tree {
	if bucketSize < 2 {
		bucketSize = 10
	}
	root := &Node{ID: "root", Content: "memory root", CreatedAt: time.Now()}
	t := &Tree{
		root:       root,
		nodeMap:    map[string]*Node{"root": root},
		bucketSize: bucketSize,
	}
	return t
}

// NewPersistentTree creates a tree backed by SQLite, migrating the schema on first use.
func NewPersistentTree(bucketSize int, db *sql.DB) (*Tree, error) {
	t := NewTree(bucketSize)
	t.db = db
	if err := t.migrate(); err != nil {
		return nil, fmt.Errorf("tree migration: %w", err)
	}
	if err := t.load(); err != nil {
		return nil, fmt.Errorf("tree load: %w", err)
	}
	return t, nil
}

// migrate creates the persistence table if it does not exist.
func (t *Tree) migrate() error {
	if t.db == nil {
		return nil
	}
	_, err := t.db.Exec(`
		CREATE TABLE IF NOT EXISTS mem_tree_nodes (
			id TEXT PRIMARY KEY,
			parent_id TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			sealed_at TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_mem_tree_parent ON mem_tree_nodes(parent_id);
	`)
	return err
}

// load hydrates the tree from persistent storage.
func (t *Tree) load() error {
	if t.db == nil {
		return nil
	}
	rows, err := t.db.Query(`SELECT id, parent_id, content, summary, count, created_at, sealed_at FROM mem_tree_nodes ORDER BY created_at ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var n Node
		var parentID, sealedStr sql.NullString
		var count int
		var createdAtStr string
		if err := rows.Scan(&n.ID, &parentID, &n.Content, &n.Summary, &count, &createdAtStr, &sealedStr); err != nil {
			return err
		}
		n.Count = count
		if parentID.Valid && parentID.String != "" {
			n.ParentID = parentID.String
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		if sealedStr.Valid && sealedStr.String != "" {
			ts, _ := time.Parse(time.RFC3339, sealedStr.String)
			n.SealedAt = &ts
		}
		if n.ID == "root" {
			t.root.Count = n.Count
			t.root.CreatedAt = n.CreatedAt
			if n.SealedAt != nil {
				t.root.SealedAt = n.SealedAt
			}
			continue
		}
		// Attach to parent.
		parent := t.nodeMap[n.ParentID]
		if parent == nil {
			parent = t.root
			n.ParentID = "root"
		}
		parent.Children = append(parent.Children, &n)
		t.nodeMap[n.ID] = &n
	}
	return rows.Err()
}

// Add inserts content into the tree under a parent node. If a DB is configured,
// the node is persisted immediately. If a node with the given ID already exists,
// its count is incremented and content updated.
func (t *Tree) Add(parentID, id, content string) (*Node, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	parent, ok := t.nodeMap[parentID]
	if !ok {
		parent = t.root
	}

	// If node already exists, increment its count and update content.
	// Do NOT increment parent.Count — the child was already counted when
	// first added; inflating the parent count causes premature sealing.
	if existing, ok := t.nodeMap[id]; ok {
		existing.Count++
		existing.Content = content
		if t.db != nil {
			if _, err := t.db.Exec(`UPDATE mem_tree_nodes SET count = ?, content = ? WHERE id = ?`,
				existing.Count, content, id); err != nil {
				slog.Warn("tree: failed to update node", "id", id, "error", err)
			}
			if _, err := t.db.Exec(`UPDATE mem_tree_nodes SET count = ? WHERE id = ?`, parent.Count, parent.ID); err != nil {
				slog.Warn("tree: failed to update parent count", "parent_id", parent.ID, "error", err)
			}
		}
		return existing, nil
	}

	node := &Node{
		ID:        id,
		ParentID:  parent.ID,
		Content:   content,
		Count:     1,
		CreatedAt: time.Now(),
	}
	parent.Children = append(parent.Children, node)
	parent.Count++
	t.nodeMap[id] = node

	// Persist.
	if t.db != nil {
		_, err := t.db.Exec(
			`INSERT OR REPLACE INTO mem_tree_nodes (id, parent_id, content, summary, count, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			node.ID, node.ParentID, node.Content, node.Summary, node.Count, node.CreatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return node, fmt.Errorf("persist tree node: %w", err)
		}
		// Update parent count in DB.
		if _, err := t.db.Exec(`UPDATE mem_tree_nodes SET count = ? WHERE id = ?`, parent.Count, parent.ID); err != nil {
			slog.Warn("tree: failed to update parent count after insert", "parent_id", parent.ID, "error", err)
		}
	}

	return node, nil
}

// Seal triggers summarization when a node exceeds bucket size.
func (t *Tree) Seal(nodeID string, summary string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	node, ok := t.nodeMap[nodeID]
	if !ok {
		return nil
	}

	now := time.Now()
	node.SealedAt = &now
	node.Summary = summary

	if t.db != nil {
		if _, err := t.db.Exec(`UPDATE mem_tree_nodes SET summary = ?, sealed_at = ?, count = ? WHERE id = ?`,
			summary, now.Format(time.RFC3339), node.Count, nodeID); err != nil {
			slog.Warn("tree: failed to persist seal", "node_id", nodeID, "error", err)
		}
	}
	return nil
}

// SealWithCascade seals a node and then checks whether the parent node has
// accumulated enough sealed children to trigger a cascade seal upward.
// When the parent's sealed child count reaches BucketSize, the parent is
// sealed with an aggregated summary and the cascade continues upward.
func (t *Tree) SealWithCascade(nodeID, summary string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sealWithCascadeLocked(nodeID, summary)
}

// sealWithCascadeLocked implements the cascade logic while the mutex is held.
// It recursively seals parent nodes without dropping and re-acquiring the lock,
// avoiding the deadlock risk when a panic occurs between Unlock/Lock.
func (t *Tree) sealWithCascadeLocked(nodeID, summary string) error {
	node, ok := t.nodeMap[nodeID]
	if !ok {
		return nil
	}
	now := time.Now()
	node.SealedAt = &now
	node.Summary = summary
	t.persistSealLocked(node)

	// Cascade upward: if parent has enough sealed children, seal the parent.
	if node.ParentID != "" && node.ParentID != "root" {
		parent := t.nodeMap[node.ParentID]
		if parent != nil {
			sealed := 0
			for _, child := range parent.Children {
				if child.SealedAt != nil {
					sealed++
				}
			}
			if sealed >= t.bucketSize {
				parentSummary := t.buildAggregateSummaryLocked(parent)
				return t.sealWithCascadeLocked(parent.ID, parentSummary)
			}
		}
	}
	return nil
}

func (t *Tree) persistSealLocked(node *Node) {
	if t.db == nil {
		return
	}
	summary := node.Summary
	now := node.SealedAt.Format(time.RFC3339)
	if _, err := t.db.Exec(
		`UPDATE mem_tree_nodes SET summary = ?, sealed_at = ?, count = ? WHERE id = ?`,
		summary, now, node.Count, node.ID); err != nil {
		slog.Warn("tree: failed to persist seal", "node_id", node.ID, "error", err)
	}
}

func (t *Tree) buildAggregateSummaryLocked(parent *Node) string {
	var parts []string
	for _, child := range parent.Children {
		if child.Summary != "" {
			parts = append(parts, child.Summary)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("aggregated %d children", len(parent.Children))
	}
	return strings.Join(parts, "; ")
}

// BucketSize returns the bucket size threshold for sealing.
func (t *Tree) BucketSize() int { return t.bucketSize }

// Get returns a node by ID.
func (t *Tree) Get(id string) *Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.nodeMap[id]
}

// ListByParent returns children of a node.
// RootSummaries returns summary strings for top-level nodes (children of root).
// Used to inject namespace-level memory context into the system prompt.
func (t *Tree) RootSummaries() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	children := make([]*Node, 0)
	for _, n := range t.nodeMap {
		if n.ParentID == "root" && n.Summary != "" {
			children = append(children, n)
		}
	}
	summaries := make([]string, 0, len(children))
	for _, n := range children {
		summaries = append(summaries, n.Summary)
	}
	return summaries
}

func (t *Tree) ListByParent(parentID string) []*Node {
	t.mu.RLock()
	defer t.mu.RUnlock()

	parent, ok := t.nodeMap[parentID]
	if !ok {
		return nil
	}
	result := make([]*Node, len(parent.Children))
	copy(result, parent.Children)
	return result
}

// Search finds nodes whose content contains the query.
func (t *Tree) Search(query string, maxResults int) []*Node {
	t.mu.RLock()
	defer t.mu.RUnlock()

	type scored struct {
		node  *Node
		score int
	}
	var results []scored

	for _, node := range t.nodeMap {
		score := matchScore(node.Content, query) + matchScore(node.Summary, query)
		if score > 0 {
			results = append(results, scored{node: node, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	nodes := make([]*Node, len(results))
	for i, r := range results {
		nodes[i] = r.node
	}
	return nodes
}

func matchScore(text, query string) int {
	if len(query) == 0 || len(text) == 0 {
		return 0
	}
	score := 0
	textLower := toLower(text)
	queryLower := toLower(query)
	for i := 0; i <= len(textLower)-len(queryLower); i++ {
		if textLower[i:i+len(queryLower)] == queryLower {
			score++
		}
	}
	return score
}

func toLower(s string) string {
	return strings.ToLower(s)
}
