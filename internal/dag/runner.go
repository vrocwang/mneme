package dag

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RunnerConfig holds dependencies for the DAG Runner.
type RunnerConfig struct {
	Executor *NodeExecutor
	Store    *Store // optional SQLite persistence
	Log      *slog.Logger
}

// Runner executes DAG graphs with topological ordering, per-node
// execution, checkpoint persistence, cancellation, and step recording.
type Runner struct {
	executor *NodeExecutor
	store    *Store
	log      *slog.Logger

	mu     sync.Mutex
	active map[string]context.CancelFunc // runID → cancel
}

// NewRunner creates a DAG Runner.
func NewRunner(cfg RunnerConfig) *Runner {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Runner{
		executor: cfg.Executor,
		store:    cfg.Store,
		log:      log,
		active:   make(map[string]context.CancelFunc),
	}
}

// Run executes a graph from the beginning. Returns a RunRecord with
// all step results.
func (r *Runner) Run(ctx context.Context, graph *Graph, input *NodeInput) (*RunRecord, error) {
	if err := graph.Validate(); err != nil {
		return nil, err
	}

	runID := fmt.Sprintf("dag_%d", time.Now().UnixNano())
	rec := &RunRecord{
		ID:        runID,
		GraphName: graph.Name,
		Status:    StatusRunning,
		Input:     input,
		StartedAt: time.Now(),
		Steps:     make([]RunStep, 0, len(graph.Nodes)),
	}

	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.active[runID] = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.active, runID)
		r.mu.Unlock()
	}()

	// Phase 1: topological sort.
	order, err := topologicalSort(graph)
	if err != nil {
		rec.Status = StatusFailed
		rec.Error = err.Error()
		rec.EndedAt = time.Now()
		r.saveRecord(rec)
		return rec, err
	}

	// Phase 2: execute nodes in order.
	items := input.Items
	if items == nil {
		items = []map[string]interface{}{{}}
	}

	for _, nodeID := range order {
		if ctx.Err() != nil {
			rec.Status = StatusCancelled
			rec.Error = "run cancelled"
			rec.EndedAt = time.Now()
			rec.StepIndex = len(rec.Steps)
			r.saveRecord(rec)
			return rec, ctx.Err()
		}

		node := graph.nodeByID(nodeID)
		if node == nil {
			continue
		}

		// Skip trigger nodes — they pass through.
		if isTriggerKind(node.Kind) {
			rec.Steps = append(rec.Steps, RunStep{
				NodeID:   node.ID,
				NodeKind: node.Kind,
				NodeName: node.Name,
				Status:   StatusCompleted,
				Output:   "trigger",
			})
			continue
		}

		// Collect inputs from incoming edges, filtered by port.
		nodeInput := collectInputs(graph, nodeID, items)
		if len(nodeInput.Items) == 0 {
			// No active inputs — this branch wasn't taken.
			rec.Steps = append(rec.Steps, RunStep{
				NodeID:   node.ID,
				NodeKind: node.Kind,
				NodeName: node.Name,
				Status:   StatusCompleted,
				Output:   "skipped (no active inputs)",
			})
			continue
		}

		// Execute the node.
		stepStart := time.Now()
		output, execErr := r.executor.Execute(ctx, *node, nodeInput)
		stepDur := time.Since(stepStart)

		step := RunStep{
			NodeID:     node.ID,
			NodeKind:   node.Kind,
			NodeName:   node.Name,
			StartedAt:  stepStart,
			EndedAt:    time.Now(),
			DurationMs: stepDur.Milliseconds(),
		}

		if execErr != nil {
			step.Status = StatusFailed
			step.Error = execErr.Error()
			rec.Steps = append(rec.Steps, step)
			rec.Status = StatusFailed
			rec.Error = fmt.Sprintf("node %q failed: %s", node.ID, execErr.Error())
			rec.EndedAt = time.Now()
			rec.StepIndex = len(rec.Steps)
			r.saveRecord(rec)
			return rec, execErr
		}

		step.Status = StatusCompleted
		step.Output = outputString(output)
		rec.Steps = append(rec.Steps, step)

		// Distribute outputs to downstream nodes.
		items = distributeOutputs(graph, nodeID, output, items)
	}

	rec.Status = StatusCompleted
	rec.EndedAt = time.Now()
	rec.StepIndex = len(rec.Steps)
	rec.Output = &NodeOutput{Items: items}
	r.saveRecord(rec)
	return rec, nil
}

// DryRun validates and compiles the graph without executing any nodes.
// Returns the compiled graph and any validation issues. This is useful
// for pre-flight checks before committing to execution.
func (r *Runner) DryRun(ctx context.Context, graph *Graph, input *NodeInput) (*CompiledGraph, []ValidationIssue, error) {
	issues := ValidateGraph(graph)
	cg, err := Compile(graph)
	if err != nil {
		return nil, issues, err
	}
	r.log.Info("dag dry-run passed", "graph", graph.Name, "nodes", cg.NodeCount, "layers", len(cg.Layers))
	return cg, issues, nil
}

// Cancel stops a running DAG run by its run ID.
func (r *Runner) Cancel(runID string) bool {
	r.mu.Lock()
	cancel, ok := r.active[runID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// ActiveCount returns the number of currently running DAG executions.
func (r *Runner) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}

func (r *Runner) saveRecord(rec *RunRecord) {
	if r.store == nil {
		return
	}
	if err := r.store.SaveRun(rec); err != nil {
		r.log.Warn("dag: failed to save run record", "run_id", rec.ID, "error", err)
	}
}

// ── Topological sort (Kahn's algorithm) ───────────────────────────────

func topologicalSort(graph *Graph) ([]string, error) {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)

	for _, n := range graph.Nodes {
		inDegree[n.ID] = 0
	}

	for _, e := range graph.Edges {
		adj[e.FromNode] = append(adj[e.FromNode], e.ToNode)
		inDegree[e.ToNode]++
	}

	var queue []string
	for _, n := range graph.Nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	var order []string
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		order = append(order, nodeID)

		for _, neighbor := range adj[nodeID] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(order) != len(graph.Nodes) {
		return nil, fmt.Errorf("dag: cycle detected in graph %q", graph.Name)
	}

	return order, nil
}

// ── Edge-aware item routing ───────────────────────────────────────────

// collectInputs gathers items from all incoming edges to nodeID, filtered
// by port routing from condition/switch nodes.
func collectInputs(graph *Graph, nodeID string, allItems []map[string]interface{}) *NodeInput {
	hasIncoming := false
	var items []map[string]interface{}

	for _, e := range graph.Edges {
		if e.ToNode != nodeID {
			continue
		}
		hasIncoming = true

		fromPort := e.FromPort
		if fromPort == "" {
			fromPort = "main"
		}

		// Find items tagged with the matching port from the source node.
		fromNode := graph.nodeByID(e.FromNode)
		if fromNode != nil && fromNode.Kind == KindCondition {
			// For condition nodes, filter by the condition result port.
			for _, item := range allItems {
				if port, _ := item["_condition_port"].(string); port == fromPort {
					items = append(items, item)
				}
			}
		} else {
			items = append(items, allItems...)
		}
	}

	if !hasIncoming {
		// Root node — use all items.
		return &NodeInput{Items: allItems}
	}

	return &NodeInput{Items: items}
}

// distributeOutputs merges node output into the global item pool, tagged
// with output port for downstream routing.
func distributeOutputs(graph *Graph, nodeID string, output *NodeOutput, allItems []map[string]interface{}) []map[string]interface{} {
	// For condition nodes, merge the condition metadata into all items
	// so downstream collection can filter by port.
	node := graph.nodeByID(nodeID)
	if node != nil && node.Kind == KindCondition && len(output.Items) > 0 {
		condItem := output.Items[0]
		port, _ := condItem["_condition_port"].(string)
		// Tag downstream-visible items.
		out := make([]map[string]interface{}, 0, len(allItems))
		for _, item := range allItems {
			tagged := mergeItem(item, map[string]interface{}{
				"_condition_port": port,
			})
			out = append(out, tagged)
		}
		return out
	}

	// For other nodes, replace the item pool with the output.
	return output.Items
}

// ── Helpers ───────────────────────────────────────────────────────────

func (g *Graph) nodeByID(id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

func isTriggerKind(k NodeKind) bool {
	switch k {
	case KindTriggerManual, KindTriggerCron, KindTriggerWebhook:
		return true
	default:
		return false
	}
}

func outputString(o *NodeOutput) string {
	if o == nil {
		return ""
	}
	b, err := jsonMarshal(o)
	if err != nil {
		return fmt.Sprintf("%v", o)
	}
	return string(b)
}
