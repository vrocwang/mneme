// Package dag provides a lightweight DAG orchestration engine for
// deterministic multi-step automations. It complements the LLM Agent
// path (BackgroundRunner) by executing non-LLM steps (HTTP calls,
// conditions, sandboxed code) as a topologically-sorted node graph.
//
// Node types: http_request, condition, code, agent, transform
// Graph format: JSON-serializable DAG with nodes[] + edges[]
// Triggers: manual (RPC), cron, webhook
package dag

import (
	"encoding/json"
	"time"
)

// NodeKind identifies the type of a DAG node.
type NodeKind string

const (
	KindHTTPRequest NodeKind = "http_request"
	KindCondition   NodeKind = "condition"
	KindCode        NodeKind = "code"
	KindAgent       NodeKind = "agent"
	KindTransform   NodeKind = "transform"

	// Trigger kinds — only valid on the first node.
	KindTriggerManual  NodeKind = "trigger_manual"
	KindTriggerCron    NodeKind = "trigger_cron"
	KindTriggerWebhook NodeKind = "trigger_webhook"
)

// Node is a single step in a DAG workflow.
type Node struct {
	ID     string                 `json:"id"`
	Kind   NodeKind               `json:"kind"`
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config"`
}

// Edge connects two nodes with optional port routing.
type Edge struct {
	FromNode string `json:"from_node"`
	ToNode   string `json:"to_node"`
	FromPort string `json:"from_port,omitempty"` // default: "main"
	ToPort   string `json:"to_port,omitempty"`   // default: "main"
}

// Graph is a DAG workflow definition.
type Graph struct {
	Name  string `json:"name"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// RunStatus tracks the lifecycle of a DAG run.
type RunStatus string

const (
	StatusPending   RunStatus = "pending"
	StatusRunning   RunStatus = "running"
	StatusCompleted RunStatus = "completed"
	StatusFailed    RunStatus = "failed"
	StatusCancelled RunStatus = "cancelled"
)

// RunRecord captures a single DAG execution.
type RunRecord struct {
	ID        string      `json:"id"`
	GraphName string      `json:"graph_name"`
	Status    RunStatus   `json:"status"`
	Input     *NodeInput  `json:"input,omitempty"`
	Output    *NodeOutput `json:"output,omitempty"`
	Error     string      `json:"error,omitempty"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at,omitempty"`
	Steps     []RunStep   `json:"steps,omitempty"`
	StepIndex int         `json:"step_index"` // checkpoint: next step to execute
}

// RunStep records a single node's execution within a run.
type RunStep struct {
	NodeID     string      `json:"node_id"`
	NodeKind   NodeKind    `json:"node_kind"`
	NodeName   string      `json:"node_name"`
	Status     RunStatus   `json:"status"`
	Output     interface{} `json:"output,omitempty"`
	Error      string      `json:"error,omitempty"`
	DurationMs int64       `json:"duration_ms"`
	StartedAt  time.Time   `json:"started_at"`
	EndedAt    time.Time   `json:"ended_at,omitempty"`
}

// NodeInput carries the items flowing into a node.
type NodeInput struct {
	Items []map[string]interface{} `json:"items"`
}

// NodeOutput carries the items emitted by a node.
type NodeOutput struct {
	Items []map[string]interface{} `json:"items"`
}

// ToJSON serialises the graph to indented JSON.
func (g *Graph) ToJSON() ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}

// GraphFromJSON deserialises a graph from JSON bytes.
func GraphFromJSON(data []byte) (*Graph, error) {
	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// Validate performs basic structural checks on the graph.
func (g *Graph) Validate() error {
	if g.Name == "" {
		return &ValidationError{Message: "graph name is required"}
	}
	if len(g.Nodes) == 0 {
		return &ValidationError{Message: "graph must have at least one node"}
	}
	if len(g.Edges) == 0 && len(g.Nodes) > 1 {
		return &ValidationError{Message: "graph with multiple nodes must have edges"}
	}

	ids := make(map[string]bool)
	for _, n := range g.Nodes {
		if n.ID == "" {
			return &ValidationError{Message: "node id is required"}
		}
		if ids[n.ID] {
			return &ValidationError{Message: "duplicate node id: " + n.ID}
		}
		ids[n.ID] = true

		if !isValidKind(n.Kind) {
			return &ValidationError{Message: "unknown node kind: " + string(n.Kind)}
		}
	}

	for _, e := range g.Edges {
		if !ids[e.FromNode] {
			return &ValidationError{Message: "edge from unknown node: " + e.FromNode}
		}
		if !ids[e.ToNode] {
			return &ValidationError{Message: "edge to unknown node: " + e.ToNode}
		}
		if e.FromNode == e.ToNode {
			return &ValidationError{Message: "self-loop not allowed: " + e.FromNode}
		}
	}

	return nil
}

// ValidationError represents a graph validation failure.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return "dag validation: " + e.Message
}

func isValidKind(k NodeKind) bool {
	switch k {
	case KindHTTPRequest, KindCondition, KindCode, KindAgent, KindTransform,
		KindTriggerManual, KindTriggerCron, KindTriggerWebhook:
		return true
	default:
		return false
	}
}
