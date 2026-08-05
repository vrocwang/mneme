package tree

import (
	"fmt"
	"time"
)

// HealthStatus reports the health of a memory tree.
type HealthStatus struct {
	Healthy     bool      `json:"healthy"`
	NodeCount   int       `json:"node_count"`
	LeafCount   int       `json:"leaf_count"`
	MaxDepth    int       `json:"max_depth"`
	EmptyNodes  int       `json:"empty_nodes"`
	StaleNodes  int       `json:"stale_nodes"` // unsealed nodes older than threshold
	LastChecked time.Time `json:"last_checked"`
}

// Check performs a health check on the tree, identifying structural issues.
func (t *Tree) Check(maxStaleAge time.Duration) HealthStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := HealthStatus{
		Healthy:     true,
		LastChecked: time.Now().UTC(),
	}

	if t.root == nil {
		status.Healthy = false
		return status
	}

	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		status.NodeCount++
		if depth > status.MaxDepth {
			status.MaxDepth = depth
		}

		// Check for empty content nodes.
		if n.Content == "" && n.Summary == "" {
			status.EmptyNodes++
		}

		// Check for stale unsealed nodes.
		if n.SealedAt.IsZero() && time.Since(n.CreatedAt) > maxStaleAge {
			status.StaleNodes++
		}

		if len(n.Children) == 0 {
			status.LeafCount++
		} else {
			for _, child := range n.Children {
				walk(child, depth+1)
			}
		}
	}

	walk(t.root, 0)

	if status.EmptyNodes > status.NodeCount/2 {
		status.Healthy = false
	}
	if status.StaleNodes > 10 {
		status.Healthy = false
	}

	return status
}

// Summary returns a human-readable health summary.
func (s HealthStatus) Summary() string {
	if s.Healthy {
		return fmt.Sprintf("Tree healthy: %d nodes, %d leaves, max depth %d",
			s.NodeCount, s.LeafCount, s.MaxDepth)
	}
	return fmt.Sprintf("Tree unhealthy: %d nodes (%d empty, %d stale)",
		s.NodeCount, s.EmptyNodes, s.StaleNodes)
}
