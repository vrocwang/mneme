package dag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/simon/mneme/internal/tools"
)

// ── dag_list ─────────────────────────────────────────────────────────

type listGraphsTool struct {
	store *Store
}

func (t *listGraphsTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "dag_list",
		Description: "List saved DAG automation workflows (graphs). Each graph defines a multi-step automation with nodes for HTTP requests, conditions, code execution, and agent prompts.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *listGraphsTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	names, err := t.store.ListGraphs()
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Failed to list graphs: %v", err)}
	}
	if len(names) == 0 {
		return tools.Result{Success: true, Output: "No saved DAG workflows. Use dag_save to create one."}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Saved workflows: %s", strings.Join(names, ", "))}
}

func (t *listGraphsTool) ConcurrencySafe() bool                  { return true }
func (t *listGraphsTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }

// ── dag_describe ─────────────────────────────────────────────────────

type describeGraphTool struct {
	store *Store
}

func (t *describeGraphTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "dag_describe",
		Description: "Describe a saved DAG workflow graph by name. Returns the full node list and edge connections.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"graph_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the graph (from dag_list).",
				},
			},
			"required": []string{"graph_name"},
		},
	}
}

func (t *describeGraphTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	name, _ := args["graph_name"].(string)
	if name == "" {
		return tools.Result{Error: "graph_name is required"}
	}

	graph, err := t.store.GetGraph(name)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Graph %q not found: %v", name, err)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", graph.Name))
	b.WriteString(fmt.Sprintf("**Nodes (%d):**\n", len(graph.Nodes)))
	for _, n := range graph.Nodes {
		b.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", n.ID, n.Kind, n.Name))
		if len(n.Config) > 0 {
			cfgJSON, _ := json.MarshalIndent(n.Config, "  ", "  ")
			b.WriteString(fmt.Sprintf("  config: %s\n", string(cfgJSON)))
		}
	}
	b.WriteString(fmt.Sprintf("\n**Edges (%d):**\n", len(graph.Edges)))
	for _, e := range graph.Edges {
		port := ""
		if e.FromPort != "" && e.FromPort != "main" {
			port = fmt.Sprintf(" [port=%s]", e.FromPort)
		}
		b.WriteString(fmt.Sprintf("- %s%s → %s\n", e.FromNode, port, e.ToNode))
	}
	return tools.Result{Success: true, Output: b.String()}
}

func (t *describeGraphTool) ConcurrencySafe() bool                  { return true }
func (t *describeGraphTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }

// ── dag_save ─────────────────────────────────────────────────────────

type saveGraphTool struct {
	store *Store
}

func (t *saveGraphTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "dag_save",
		Description: "Save a DAG automation workflow graph. The graph defines nodes and edges for multi-step execution. Node kinds: http_request (config: method, url, headers?, body?), condition (config: field, op, value), code (config: language, source), agent (config: prompt), transform (config: set).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Unique name for this workflow graph.",
				},
				"graph_json": map[string]interface{}{
					"type":        "string",
					"description": "JSON graph definition with nodes and edges arrays.",
				},
			},
			"required": []string{"name", "graph_json"},
		},
	}
}

func (t *saveGraphTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	name, _ := args["name"].(string)
	graphJSON, _ := args["graph_json"].(string)
	if name == "" || graphJSON == "" {
		return tools.Result{Error: "name and graph_json are required"}
	}

	graph, err := GraphFromJSON([]byte(graphJSON))
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Invalid graph JSON: %v", err)}
	}
	graph.Name = name

	if err := graph.Validate(); err != nil {
		return tools.Result{Error: fmt.Sprintf("Graph validation failed: %v", err)}
	}

	if err := t.store.SaveGraph(graph); err != nil {
		return tools.Result{Error: fmt.Sprintf("Failed to save graph: %v", err)}
	}

	return tools.Result{Success: true, Output: fmt.Sprintf("Graph %q saved with %d nodes.", name, len(graph.Nodes))}
}

func (t *saveGraphTool) PermissionLevel() tools.PermissionLevel { return tools.PermWrite }
func (t *saveGraphTool) ConcurrencySafe() bool                  { return false }

// ── dag_run ──────────────────────────────────────────────────────────

type runGraphTool struct {
	runner *Runner
	store  *Store
}

func (t *runGraphTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "dag_run",
		Description: "Execute a saved DAG workflow graph by name. Returns a run_id for tracking. Use dag_run_status to check progress.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"graph_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the graph to execute (from dag_list).",
				},
				"inputs": map[string]interface{}{
					"type":        "object",
					"description": "Optional key-value inputs for the first node.",
				},
			},
			"required": []string{"graph_name"},
		},
	}
}

func (t *runGraphTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	if t.runner == nil {
		return tools.Result{Error: "dag runner not available — DAG engine is still initialising"}
	}
	name, _ := args["graph_name"].(string)
	if name == "" {
		return tools.Result{Error: "graph_name is required"}
	}

	graph, err := t.store.GetGraph(name)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Graph %q not found: %v", name, err)}
	}

	input := &NodeInput{}
	if inputsRaw, ok := args["inputs"].(map[string]interface{}); ok && len(inputsRaw) > 0 {
		item := make(map[string]interface{})
		for k, v := range inputsRaw {
			item[k] = v
		}
		input.Items = []map[string]interface{}{item}
	}

	rec, err := t.runner.Run(ctx, graph, input)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Graph execution failed: %v", err)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Graph %q completed with status: %s\n", name, rec.Status))
	b.WriteString(fmt.Sprintf("Run ID: %s\n", rec.ID))
	if rec.Error != "" {
		b.WriteString(fmt.Sprintf("Error: %s\n", rec.Error))
	}
	if len(rec.Steps) > 0 {
		b.WriteString("\nSteps:\n")
		for _, s := range rec.Steps {
			b.WriteString(fmt.Sprintf("- %s (%s): %s", s.NodeName, s.Status, s.NodeKind))
			if s.Error != "" {
				b.WriteString(fmt.Sprintf(" — %s", s.Error))
			}
			b.WriteString(fmt.Sprintf(" (%dms)\n", s.DurationMs))
		}
	}
	return tools.Result{Success: true, Output: b.String()}
}

func (t *runGraphTool) PermissionLevel() tools.PermissionLevel { return tools.PermExecute }
func (t *runGraphTool) ConcurrencySafe() bool                  { return false }

// ── dag_run_status ───────────────────────────────────────────────────

type runStatusTool struct {
	store *Store
}

func (t *runStatusTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "dag_run_status",
		Description: "Check the status of a DAG workflow run by run_id. Returns status, steps, and output.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"run_id": map[string]interface{}{
					"type":        "string",
					"description": "Run ID returned by dag_run.",
				},
			},
			"required": []string{"run_id"},
		},
	}
}

func (t *runStatusTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return tools.Result{Error: "run_id is required"}
	}

	rec, err := t.store.GetRun(runID)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("Run %q not found: %v", runID, err)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Run: %s\n", rec.ID))
	b.WriteString(fmt.Sprintf("Graph: %s\n", rec.GraphName))
	b.WriteString(fmt.Sprintf("Status: %s\n", rec.Status))
	if rec.Error != "" {
		b.WriteString(fmt.Sprintf("Error: %s\n", rec.Error))
	}
	b.WriteString(fmt.Sprintf("Started: %s\n", rec.StartedAt.Format("15:04:05")))
	if !rec.EndedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Ended: %s\n", rec.EndedAt.Format("15:04:05")))
	}
	if len(rec.Steps) > 0 {
		b.WriteString("\nSteps:\n")
		for _, s := range rec.Steps {
			b.WriteString(fmt.Sprintf("- %s: %s (%dms)\n", s.NodeName, s.Status, s.DurationMs))
		}
	}
	return tools.Result{Success: true, Output: b.String()}
}

func (t *runStatusTool) ConcurrencySafe() bool                  { return true }
func (t *runStatusTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }

// ── Registration ─────────────────────────────────────────────────────

// RegisterTools registers all DAG tools into the capability registry.
func RegisterTools(reg interface {
	RegisterTool(setID string, t tools.Tool)
}, runner *Runner, store *Store) {
	if reg == nil {
		return
	}

	reg.RegisterTool("builtin", &listGraphsTool{store: store})
	reg.RegisterTool("builtin", &describeGraphTool{store: store})
	reg.RegisterTool("builtin", &saveGraphTool{store: store})
	reg.RegisterTool("builtin", &runGraphTool{runner: runner, store: store})
	reg.RegisterTool("builtin", &runStatusTool{store: store})
}
