// Workflow run tools. Ported verbatim from internal/workflows/tools.go, plus
// the extension-only workflow_cancel tool (see run.go for why cancel exists).
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon/mneme/pkg/extsdk"
)

// ── describe_workflow ─────────────────────────────────────────────────

// describeWorkflowTool describes a single workflow by ID.
type describeWorkflowTool struct {
	userDir    string
	projectDir string
}

func (t *describeWorkflowTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name:        "describe_workflow",
		Description: "Describe one workflow by `workflow_id`: its definition, description, inputs, and phases. Use before running to learn which inputs to supply.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"workflow_id": map[string]interface{}{
					"type":        "string",
					"description": "Workflow id (directory name from workflow_list).",
				},
			},
			"required": []string{"workflow_id"},
		},
		Permission: permReadOnly,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *describeWorkflowTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	wfID, _ := args["workflow_id"].(string)
	if wfID == "" {
		// Accept legacy "skill_id" alias.
		wfID, _ = args["skill_id"].(string)
	}
	if wfID == "" {
		return extsdk.Result{Error: "workflow_id is required"}
	}

	workflows := DiscoverWorkflows(t.userDir, t.projectDir)
	var wf *Workflow
	for _, w := range workflows {
		if w.DirName == wfID {
			wf = w
			break
		}
	}
	if wf == nil {
		return extsdk.Result{Error: fmt.Sprintf("workflow %q not found", wfID)}
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
	return extsdk.Result{Success: true, Output: b.String()}
}

// ── run_workflow ──────────────────────────────────────────────────────

// runWorkflowTool spawns a workflow run and guides the agent through phases.
type runWorkflowTool struct {
	runner     *Runner
	userDir    string
	projectDir string
}

func (t *runWorkflowTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
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
		Permission: permExecute,
		HasEffects: true,
	}
}

func (t *runWorkflowTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	wfID, _ := args["workflow_id"].(string)
	if wfID == "" {
		return extsdk.Result{Error: "workflow_id is required"}
	}

	inputs := make(map[string]string)
	if inputsRaw, ok := args["inputs"].(map[string]interface{}); ok {
		for k, v := range inputsRaw {
			inputs[k] = fmt.Sprint(v)
		}
	}

	// Look up the workflow to provide phase guidance.
	workflows := DiscoverWorkflows(t.userDir, t.projectDir)
	var wf *Workflow
	for _, w := range workflows {
		if w.DirName == wfID {
			wf = w
			break
		}
	}
	if wf == nil {
		return extsdk.Result{Error: fmt.Sprintf("workflow %q not found", wfID)}
	}

	phaseNames := wf.PhaseNames()
	phaseCount := len(wf.Phases)

	req := RunRequest{WorkflowID: wfID, Inputs: inputs}
	runID, err := t.runner.Spawn(req, func(ctx context.Context, r RunRequest, logWriter func(string)) (string, error) {
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

		// Wait for the agent to call complete_workflow (or workflow_cancel).
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("workflow timed out after running %d phases", phaseCount)
		}
		return fmt.Sprintf("Workflow %q completed with %d phases", wf.Name, phaseCount), nil
	})
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("spawn workflow: %v", err)}
	}

	return extsdk.Result{
		Success: true,
		Output:  fmt.Sprintf("Workflow started.\n  run_id: %s\n  workflow_id: %s\n  phases: %d\n\nUse workflow_read to get phase details. Use await_workflow with this run_id to wait for completion. Call complete_workflow when done.", runID, wfID, phaseCount),
	}
}

// ── complete_workflow ─────────────────────────────────────────────────

// completeWorkflowTool marks a workflow run as completed.
type completeWorkflowTool struct {
	runner *Runner
}

func (t *completeWorkflowTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
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
		// Core: PermissionLevel = execute, SideEffects not declared (false).
		Permission: permExecute,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *completeWorkflowTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return extsdk.Result{Error: "run_id is required"}
	}
	output, _ := args["output"].(string)
	if err := t.runner.Complete(runID, output); err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Workflow run %s completed.", runID)}
}

// ── await_workflow ────────────────────────────────────────────────────

// awaitWorkflowTool waits for a workflow run to complete.
type awaitWorkflowTool struct {
	runner *Runner
}

func (t *awaitWorkflowTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name: "await_workflow",
		// Description is byte-identical to the core's; the cancel relationship
		// is documented on workflow_cancel and in manifest.json instead.
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
		// Core: no PermissionLevel method at all → zero value PermNone.
		Permission: permNone,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *awaitWorkflowTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return extsdk.Result{Error: "run_id is required"}
	}

	record, err := t.runner.Await(ctx, runID)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("await: %v", err)}
	}

	return extsdk.Result{
		Success: true,
		Output: fmt.Sprintf("Workflow complete.\n  run_id: %s\n  workflow_id: %s\n  status: %s\n  duration: %dms\n  error: %s",
			record.RunID, record.WorkflowID, record.Status, record.DurationMs, record.Error),
	}
}

// ── workflow_cancel (extension-only) ──────────────────────────────────

// cancelWorkflowTool stops an in-flight workflow run.
type cancelWorkflowTool struct {
	runner *Runner
}

func (t *cancelWorkflowTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name: "workflow_cancel",
		Description: "Cancel an in-flight workflow run by `run_id`, returning it to status `cancelled`. " +
			"This tool exists because the host cannot cancel an extension call in flight (the protocol is one request per response and no context is forwarded): it flips the run's per-run cancel flag inside the extension, so a blocked `await_workflow` on the same run_id returns promptly instead of waiting out the run timeout. Use it when a workflow must be stopped early.",
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
		Permission: permExecute,
		HasEffects: true,
	}
}

func (t *cancelWorkflowTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return extsdk.Result{Error: "run_id is required"}
	}
	if err := t.runner.Cancel(runID); err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Workflow run %s cancelled (any await_workflow on this run_id returns immediately).", runID)}
}

// ── list_workflow_runs ────────────────────────────────────────────────

// listWorkflowRunsTool lists recent workflow runs.
type listWorkflowRunsTool struct {
	runner *Runner
}

func (t *listWorkflowRunsTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
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
		Permission: permReadOnly,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *listWorkflowRunsTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	wfID, _ := args["workflow_id"].(string)
	if wfID == "" {
		wfID, _ = args["skill_id"].(string) // legacy alias
	}
	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	runs := t.runner.ListRecent(wfID, limit)
	if len(runs) == 0 {
		return extsdk.Result{Success: true, Output: "No workflow runs found."}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Workflow runs (%d):\n\n", len(runs)))
	for _, r := range runs {
		b.WriteString(fmt.Sprintf("- run_id=%s workflow=%s status=%s started=%s duration=%dms\n",
			r.RunID, r.WorkflowID, r.Status, r.StartedAt.Format("15:04:05"), r.DurationMs))
	}
	return extsdk.Result{Success: true, Output: b.String()}
}

// ── read_workflow_run_log ─────────────────────────────────────────────

// readWorkflowRunLogTool reads a slice of a run log.
type readWorkflowRunLogTool struct {
	runner *Runner
}

func (t *readWorkflowRunLogTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
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
		Permission: permReadOnly,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *readWorkflowRunLogTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return extsdk.Result{Error: "run_id is required"}
	}

	rec, ok := t.runner.Status(runID)
	if !ok {
		return extsdk.Result{Error: fmt.Sprintf("run %q not found", runID)}
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
		return extsdk.Result{Error: fmt.Sprintf("read log: %v", err)}
	}

	output := fmt.Sprintf("run_id: %s\nstatus: %s\noffset: %d\nnext_offset: %d\neof: %v\n\n%s",
		runID, rec.Status, slice.Offset, slice.NextOffset, slice.EOF, slice.Content)
	return extsdk.Result{Success: true, Output: output}
}
