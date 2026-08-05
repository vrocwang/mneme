package workflows

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon/mneme/internal/agent_workflows"
	"github.com/simon/mneme/internal/tools"
)

// ── list_workflows ────────────────────────────────────────────────────

// ListWorkflowsTool lists installed workflows.
type ListWorkflowsTool struct {
	UserDir    string
	ProjectDir string
}

func (t *ListWorkflowsTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "list_workflows",
		Description: "List installed workflows (reusable, packaged agent procedures). Returns each workflow's name, dir, description, tags, and scope.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *ListWorkflowsTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	workflows := agent_workflows.DiscoverWorkflows(t.UserDir, t.ProjectDir)
	if len(workflows) == 0 {
		return tools.Result{Success: true, Output: "No workflows installed."}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Installed workflows (%d):\n\n", len(workflows)))
	for _, w := range workflows {
		b.WriteString(fmt.Sprintf("- **%s** (`%s`)\n", w.Name, w.DirName))
		if w.Description != "" {
			b.WriteString(fmt.Sprintf("  %s\n", w.Description))
		}
		b.WriteString(fmt.Sprintf("  scope=%s", w.Scope))
		if len(w.Tags) > 0 {
			b.WriteString(fmt.Sprintf(", tags=%s", strings.Join(w.Tags, ",")))
		}
		b.WriteString("\n\n")
	}
	return tools.Result{Success: true, Output: b.String()}
}

func (t *ListWorkflowsTool) ConcurrencySafe() bool                  { return true }
func (t *ListWorkflowsTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }

// ── describe_workflow ─────────────────────────────────────────────────

// DescribeWorkflowTool describes a single workflow by ID.
type DescribeWorkflowTool struct {
	UserDir    string
	ProjectDir string
}

func (t *DescribeWorkflowTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "describe_workflow",
		Description: "Describe one workflow by `workflow_id`: its definition, description, inputs, and phases. Use before running to learn which inputs to supply.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"workflow_id": map[string]interface{}{
					"type":        "string",
					"description": "Workflow id (directory name from list_workflows).",
				},
			},
			"required": []string{"workflow_id"},
		},
	}
}

func (t *DescribeWorkflowTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	wfID, _ := args["workflow_id"].(string)
	if wfID == "" {
		// Accept legacy "skill_id" alias.
		wfID, _ = args["skill_id"].(string)
	}
	if wfID == "" {
		return tools.Result{Error: "workflow_id is required"}
	}

	workflows := agent_workflows.DiscoverWorkflows(t.UserDir, t.ProjectDir)
	var wf *agent_workflows.Workflow
	for _, w := range workflows {
		if w.DirName == wfID {
			wf = w
			break
		}
	}
	if wf == nil {
		return tools.Result{Error: fmt.Sprintf("workflow %q not found", wfID)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", wf.Name))
	b.WriteString(fmt.Sprintf("**ID:** %s\n", wf.DirName))
	b.WriteString(fmt.Sprintf("**Scope:** %s\n", wf.Scope))
	if wf.Description != "" {
		b.WriteString(fmt.Sprintf("\n%s\n", wf.Description))
	}
	if len(wf.Tags) > 0 {
		b.WriteString(fmt.Sprintf("\n**Tags:** %s\n", strings.Join(wf.Tags, ", ")))
	}
	if len(wf.Tools.Allow) > 0 {
		b.WriteString(fmt.Sprintf("**Allowed tools:** %s\n", strings.Join(wf.Tools.Allow, ", ")))
	}
	if len(wf.Phases) > 0 {
		b.WriteString(fmt.Sprintf("\n**Phases (%d):**\n", len(wf.Phases)))
		for name, phase := range wf.Phases {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", name, phase.Description))
		}
	}
	return tools.Result{Success: true, Output: b.String()}
}

func (t *DescribeWorkflowTool) ConcurrencySafe() bool                  { return true }
func (t *DescribeWorkflowTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }

// ── run_workflow ──────────────────────────────────────────────────────

// RunWorkflowTool spawns a workflow run and guides the agent through phases.
type RunWorkflowTool struct {
	Runner     *Runner
	UserDir    string
	ProjectDir string
}

func (t *RunWorkflowTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "run_workflow",
		Description: "Run an installed workflow by `workflow_id`. Returns a `run_id` for tracking. Use `await_workflow` to wait for completion or `list_workflow_runs` to check status.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"workflow_id": map[string]interface{}{
					"type":        "string",
					"description": "Workflow id (directory name) to run.",
				},
				"inputs": map[string]interface{}{
					"type":        "object",
					"description": "Optional key-value inputs for the workflow.",
				},
			},
			"required": []string{"workflow_id"},
		},
	}
}

func (t *RunWorkflowTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	wfID, _ := args["workflow_id"].(string)
	if wfID == "" {
		return tools.Result{Error: "workflow_id is required"}
	}

	inputs := make(map[string]string)
	if inputsRaw, ok := args["inputs"].(map[string]interface{}); ok {
		for k, v := range inputsRaw {
			inputs[k] = fmt.Sprint(v)
		}
	}

	// Look up the workflow to provide phase guidance.
	workflows := agent_workflows.DiscoverWorkflows(t.UserDir, t.ProjectDir)
	var wf *agent_workflows.Workflow
	for _, w := range workflows {
		if w.DirName == wfID {
			wf = w
			break
		}
	}
	if wf == nil {
		return tools.Result{Error: fmt.Sprintf("workflow %q not found", wfID)}
	}

	phaseNames := wf.PhaseNames()
	phaseCount := len(wf.Phases)

	req := RunRequest{WorkflowID: wfID, Inputs: inputs}
	runID, err := t.Runner.Spawn(req, func(ctx context.Context, r RunRequest, logWriter func(string)) (string, error) {
		logWriter(fmt.Sprintf("Workflow %q started (%d phases: %s)", wf.Name, phaseCount, strings.Join(phaseNames, ", ")))

		// Write phase guidance for each phase to the log so the agent can
		// read it via read_workflow_run_log.
		for phaseIdx, name := range phaseNames {
			phase := wf.Phases[name]
			logWriter(fmt.Sprintf("\n--- Phase %d/%d: %s ---", phaseIdx+1, phaseCount, name))
			if phase.Description != "" {
				logWriter(phase.Description)
			}
			if phase.Rules != "" {
				logWriter("Rules: " + phase.Rules)
			}
		}
		logWriter("\n--- Ready ---")
		logWriter("Use workflow_read to see full workflow details. Call complete_workflow when all phases are done.")

		// Wait for the agent to call complete_workflow.
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("workflow timed out after running %d phases", phaseCount)
		}
		return fmt.Sprintf("Workflow %q completed with %d phases", wf.Name, phaseCount), nil
	})
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("spawn workflow: %v", err)}
	}

	return tools.Result{
		Success: true,
		Output:  fmt.Sprintf("Workflow started.\n  run_id: %s\n  workflow_id: %s\n  phases: %d\n\nUse workflow_read to get phase details. Use await_workflow with this run_id to wait for completion. Call complete_workflow when done.", runID, wfID, phaseCount),
	}
}

func (t *RunWorkflowTool) PermissionLevel() tools.PermissionLevel { return tools.PermExecute }
func (t *RunWorkflowTool) SideEffects() bool                      { return true }

// ── complete_workflow ─────────────────────────────────────────────────

// CompleteWorkflowTool marks a workflow run as completed.
type CompleteWorkflowTool struct {
	Runner *Runner
}

func (t *CompleteWorkflowTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "complete_workflow",
		Description: "Mark a workflow run as successfully completed by its run_id. Call this after the agent has finished executing all workflow phases.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"run_id": map[string]interface{}{
					"type":        "string",
					"description": "Run ID from run_workflow.",
				},
				"output": map[string]interface{}{
					"type":        "string",
					"description": "Brief summary of what was accomplished.",
				},
			},
			"required": []string{"run_id"},
		},
	}
}

func (t *CompleteWorkflowTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return tools.Result{Error: "run_id is required"}
	}
	output, _ := args["output"].(string)
	if err := t.Runner.Complete(runID, output); err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Workflow run %s completed.", runID)}
}

func (t *CompleteWorkflowTool) PermissionLevel() tools.PermissionLevel { return tools.PermExecute }
func (t *CompleteWorkflowTool) ConcurrencySafe() bool                  { return true }

// ── await_workflow ────────────────────────────────────────────────────

// AwaitWorkflowTool waits for a workflow run to complete.
type AwaitWorkflowTool struct {
	Runner *Runner
}

func (t *AwaitWorkflowTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "await_workflow",
		Description: "Wait for a workflow run to complete by `run_id`. Returns the final status, duration, and error if any. Use after `run_workflow`.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"run_id": map[string]interface{}{
					"type":        "string",
					"description": "Run ID from run_workflow.",
				},
			},
			"required": []string{"run_id"},
		},
	}
}

func (t *AwaitWorkflowTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return tools.Result{Error: "run_id is required"}
	}

	record, err := t.Runner.Await(ctx, runID)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("await: %v", err)}
	}

	return tools.Result{
		Success: true,
		Output: fmt.Sprintf("Workflow complete.\n  run_id: %s\n  workflow_id: %s\n  status: %s\n  duration: %dms\n  error: %s",
			record.RunID, record.WorkflowID, record.Status, record.DurationMs, record.Error),
	}
}

func (t *AwaitWorkflowTool) ConcurrencySafe() bool { return true }

// ── list_workflow_runs ────────────────────────────────────────────────

// ListWorkflowRunsTool lists recent workflow runs.
type ListWorkflowRunsTool struct {
	Runner *Runner
}

func (t *ListWorkflowRunsTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "list_workflow_runs",
		Description: "List recent workflow runs, newest first. Optionally filter by `workflow_id`. Each carries run_id, workflow_id, status, start time, and duration.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"workflow_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter to one workflow (optional).",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Max runs to return (default 20).",
				},
			},
		},
	}
}

func (t *ListWorkflowRunsTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	wfID, _ := args["workflow_id"].(string)
	if wfID == "" {
		wfID, _ = args["skill_id"].(string) // legacy alias
	}
	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	runs := t.Runner.ListRecent(wfID, limit)
	if len(runs) == 0 {
		return tools.Result{Success: true, Output: "No workflow runs found."}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Workflow runs (%d):\n\n", len(runs)))
	for _, r := range runs {
		b.WriteString(fmt.Sprintf("- run_id=%s workflow=%s status=%s started=%s duration=%dms\n",
			r.RunID, r.WorkflowID, r.Status, r.StartedAt.Format("15:04:05"), r.DurationMs))
	}
	return tools.Result{Success: true, Output: b.String()}
}

func (t *ListWorkflowRunsTool) ConcurrencySafe() bool                  { return true }
func (t *ListWorkflowRunsTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }

// ── read_workflow_run_log ─────────────────────────────────────────────

// ReadWorkflowRunLogTool reads a slice of a run log.
type ReadWorkflowRunLogTool struct {
	Runner *Runner
}

func (t *ReadWorkflowRunLogTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "read_workflow_run_log",
		Description: "Read a slice of a workflow run's log by `run_id`, from `offset` bytes up to `max_bytes`. Returns content + next offset + eof flag. Use list_workflow_runs to find run IDs.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"run_id":    map[string]interface{}{"type": "string", "description": "Workflow run id."},
				"offset":    map[string]interface{}{"type": "integer", "description": "Byte offset (default 0)."},
				"max_bytes": map[string]interface{}{"type": "integer", "description": "Max bytes to read (default 65536)."},
			},
			"required": []string{"run_id"},
		},
	}
}

func (t *ReadWorkflowRunLogTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return tools.Result{Error: "run_id is required"}
	}

	rec, ok := t.Runner.Status(runID)
	if !ok {
		return tools.Result{Error: fmt.Sprintf("run %q not found", runID)}
	}

	offset := int64(0)
	if o, ok := args["offset"].(float64); ok {
		offset = int64(o)
	}
	maxBytes := defaultMaxLogBytes
	if mb, ok := args["max_bytes"].(float64); ok && mb > 0 {
		maxBytes = int(mb)
	}

	slice, err := ReadLogSlice(rec.LogPath, offset, maxBytes)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("read log: %v", err)}
	}

	output := fmt.Sprintf("run_id: %s\nstatus: %s\noffset: %d\nnext_offset: %d\neof: %v\n\n%s",
		runID, rec.Status, slice.Offset, slice.NextOffset, slice.EOF, slice.Content)
	return tools.Result{Success: true, Output: output}
}

func (t *ReadWorkflowRunLogTool) ConcurrencySafe() bool                  { return true }
func (t *ReadWorkflowRunLogTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }
