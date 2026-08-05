package dag

import (
	"fmt"
	"strings"
)

// CompiledGraph is a validated and optimized graph ready for execution.
// It is produced by Compile() after ValidateGraph() passes.
type CompiledGraph struct {
	Source         *Graph     // original graph definition
	ExecOrder      []string   // topological execution order (node IDs)
	Layers         [][]string // parallel execution layers (nodes that can run concurrently)
	EntryNodes     []string   // nodes with no incoming edges
	ExitNodes      []string   // nodes with no outgoing edges
	NodeCount      int
	EdgeCount      int
	MaxParallelism int // maximum number of nodes in any single layer
}

// Compile validates and compiles a graph into a CompiledGraph ready for
// execution. Returns an error if validation finds any SeverityError issues.
func Compile(graph *Graph) (*CompiledGraph, error) {
	// Step 1: deep validation.
	issues := ValidateGraph(graph)
	if HasErrors(issues) {
		return nil, fmt.Errorf("compile failed:\n%s", FormatIssues(issues))
	}

	// Step 2: topological sort (reuses the existing algorithm).
	order, err := topologicalSort(graph)
	if err != nil {
		return nil, fmt.Errorf("compile: topological sort failed: %w", err)
	}

	// Step 3: compute parallel execution layers.
	layers := computeLayers(graph)

	// Step 4: identify entry and exit nodes.
	inDeg, outDeg := computeDegrees(graph)
	var entries, exits []string
	for _, n := range graph.Nodes {
		if inDeg[n.ID] == 0 {
			entries = append(entries, n.ID)
		}
		if outDeg[n.ID] == 0 {
			exits = append(exits, n.ID)
		}
	}

	maxPar := 0
	for _, layer := range layers {
		if len(layer) > maxPar {
			maxPar = len(layer)
		}
	}

	return &CompiledGraph{
		Source:         graph,
		ExecOrder:      order,
		Layers:         layers,
		EntryNodes:     entries,
		ExitNodes:      exits,
		NodeCount:      len(graph.Nodes),
		EdgeCount:      len(graph.Edges),
		MaxParallelism: maxPar,
	}, nil
}

// computeLayers groups nodes into parallel execution layers using a modified
// Kahn's algorithm. Nodes in the same layer have no dependencies on each other
// and can potentially be executed concurrently.
func computeLayers(graph *Graph) [][]string {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)

	for _, n := range graph.Nodes {
		inDegree[n.ID] = 0
	}
	for _, e := range graph.Edges {
		adj[e.FromNode] = append(adj[e.FromNode], e.ToNode)
		inDegree[e.ToNode]++
	}

	var layers [][]string
	queue := make([]string, 0)
	for _, n := range graph.Nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	for len(queue) > 0 {
		layers = append(layers, append([]string{}, queue...))
		var nextQueue []string
		for _, nodeID := range queue {
			for _, neighbor := range adj[nodeID] {
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					nextQueue = append(nextQueue, neighbor)
				}
			}
		}
		queue = nextQueue
	}

	return layers
}

// computeDegrees returns in-degree and out-degree maps for all nodes.
func computeDegrees(graph *Graph) (inDeg, outDeg map[string]int) {
	inDeg = make(map[string]int)
	outDeg = make(map[string]int)
	for _, n := range graph.Nodes {
		inDeg[n.ID] = 0
		outDeg[n.ID] = 0
	}
	for _, e := range graph.Edges {
		outDeg[e.FromNode]++
		inDeg[e.ToNode]++
	}
	return
}

// Summary returns a human-readable summary of the compiled graph.
func (c *CompiledGraph) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "compiled graph %q: %d nodes, %d edges, %d layers, max parallelism=%d",
		c.Source.Name, c.NodeCount, c.EdgeCount, len(c.Layers), c.MaxParallelism)
	if len(c.EntryNodes) > 0 {
		fmt.Fprintf(&b, "\n  entry: %s", strings.Join(c.EntryNodes, ", "))
	}
	if len(c.ExitNodes) > 0 {
		fmt.Fprintf(&b, "\n  exit: %s", strings.Join(c.ExitNodes, ", "))
	}
	for i, layer := range c.Layers {
		fmt.Fprintf(&b, "\n  layer %d: %s", i, strings.Join(layer, ", "))
	}
	return b.String()
}
