// WORKFLOW.md discovery/authoring tools. Ported verbatim from
// internal/workflows/workflow_tools.go: same tool names, descriptions, JSON
// schemas and permission/effect flags as the core's capability.SetWorkflows
// registration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/simon/mneme/pkg/extsdk"
)

// Permission strings accepted by the host (internal/tools.ParsePermissionLevel).
const (
	permNone     = "none"
	permReadOnly = "read_only"
	permWrite    = "write"
	permExecute  = "execute"
)

// extTool is a ported core tool: def() is the core's Schema() (carrying the
// permission/effect flags that the core expressed through optional
// PermissionLevel()/SideEffects() methods) and run() is its Execute().
type extTool interface {
	def() extsdk.ToolDef
	run(ctx context.Context, args map[string]interface{}) extsdk.Result
}

type toolEntry struct {
	def extsdk.ToolDef
	run extsdk.ToolHandler
}

func entry(t extTool) toolEntry { return toolEntry{def: t.def(), run: t.run} }

// registerTools installs the full workflow surface. Order matches the core's
// RegisterAll (catalog tools first, then describe/run/complete/await, with the
// extension-only workflow_cancel inserted after await_workflow). The SDK server
// is the only registry: it validates names (panicking on duplicates), owns the
// manifest tool list, and serves the protocol loop.
func registerTools(srv *extsdk.Server, runner *Runner, userDir, projectDir string) {
	listTool := &listWorkflowsTool{userDir: userDir, projectDir: projectDir}
	for _, e := range []toolEntry{
		entry(listTool),
		entry(&readWorkflowTool{listTool: listTool}),
		entry(&phaseGuidanceTool{listTool: listTool}),
		entry(&createWorkflowTool{userDir: userDir}),
		entry(&bestMatchTool{listTool: listTool}),
		entry(&describeWorkflowTool{userDir: userDir, projectDir: projectDir}),
		entry(&runWorkflowTool{runner: runner, userDir: userDir, projectDir: projectDir}),
		entry(&completeWorkflowTool{runner: runner}),
		entry(&awaitWorkflowTool{runner: runner}),
		entry(&cancelWorkflowTool{runner: runner}),
		entry(&listWorkflowRunsTool{runner: runner}),
		entry(&readWorkflowRunLogTool{runner: runner}),
	} {
		srv.RegisterTool(e.def, e.run)
	}
}

// ── workflow_list ─────────────────────────────────────────────────────

// listWorkflowsTool lists available workflows.
type listWorkflowsTool struct {
	userDir    string
	projectDir string
}

func (t *listWorkflowsTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name:        "workflow_list",
		Description: "List available WORKFLOW.md workflows that provide phase-keyed guidance for complex tasks. Use this to discover task-specific workflows before starting work.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Permission: permReadOnly,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *listWorkflowsTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	workflows := DiscoverWorkflows(t.userDir, t.projectDir)

	if len(workflows) == 0 {
		return extsdk.Result{Success: true, Output: "No workflows found."}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Available workflows (%d):\n\n", len(workflows)))
	for _, w := range workflows {
		b.WriteString(fmt.Sprintf("- **%s** (%s)\n", w.Name, w.DirName))
		if w.Description != "" {
			b.WriteString(fmt.Sprintf("  %s\n", w.Description))
		}
		// Scope (user vs project) used to be shown only by the duplicate
		// list_workflows tool; it is surfaced here so the merge loses nothing.
		b.WriteString(fmt.Sprintf("  Scope: %s\n", w.Scope))
		if len(w.Phases) > 0 {
			b.WriteString(fmt.Sprintf("  Phases: %s\n", strings.Join(w.PhaseNames(), ", ")))
		}
		if len(w.Tags) > 0 {
			b.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(w.Tags, ", ")))
		}
		b.WriteString("\n")
	}
	return extsdk.Result{Success: true, Output: b.String()}
}

// ── workflow_read ─────────────────────────────────────────────────────

// readWorkflowTool reads a specific workflow by directory name.
type readWorkflowTool struct {
	listTool *listWorkflowsTool
}

func (t *readWorkflowTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name:        "workflow_read",
		Description: "Read a specific WORKFLOW.md to get its phases, rules, tool scopes, and guidance. Use the dir_name from workflow_list results.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"dir_name": map[string]interface{}{
					"type":        "string",
					"description": "The directory name of the workflow (from workflow_list output).",
				},
			},
			"required": []string{"dir_name"},
		},
		Permission: permReadOnly,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *readWorkflowTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	dirName, _ := args["dir_name"].(string)
	if dirName == "" {
		return extsdk.Result{Error: "dir_name is required"}
	}

	// Find the workflow.
	workflows := DiscoverWorkflows(t.listTool.userDir, t.listTool.projectDir)
	var wf *Workflow
	for _, w := range workflows {
		if w.DirName == dirName {
			wf = w
			break
		}
	}
	if wf == nil {
		return extsdk.Result{Error: fmt.Sprintf("workflow %q not found", dirName)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", wf.Name))
	if wf.Description != "" {
		b.WriteString(fmt.Sprintf("%s\n\n", wf.Description))
	}
	if len(wf.Tags) > 0 {
		b.WriteString(fmt.Sprintf("**Tags:** %s\n\n", strings.Join(wf.Tags, ", ")))
	}
	if len(wf.Tools.Allow) > 0 {
		b.WriteString(fmt.Sprintf("**Allowed tools:** %s\n\n", strings.Join(wf.Tools.Allow, ", ")))
	}

	for name, phase := range wf.Phases {
		b.WriteString(fmt.Sprintf("## Phase: %s\n\n", name))
		if phase.Description != "" {
			b.WriteString(fmt.Sprintf("%s\n\n", phase.Description))
		}
		if phase.Rules != "" {
			b.WriteString(fmt.Sprintf("**Rules:**\n%s\n\n", phase.Rules))
		}
	}
	return extsdk.Result{Success: true, Output: b.String()}
}

// ── workflow_phase ────────────────────────────────────────────────────

// phaseGuidanceTool resolves phase-specific guidance and tool scope.
type phaseGuidanceTool struct {
	listTool *listWorkflowsTool
}

func (t *phaseGuidanceTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name:        "workflow_phase",
		Description: "Get phase-specific guidance and tool scope for a workflow phase. Use this when starting a new phase to understand what rules and tools apply.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"dir_name": map[string]interface{}{
					"type":        "string",
					"description": "The directory name of the workflow.",
				},
				"phase": map[string]interface{}{
					"type":        "string",
					"description": "The phase name (e.g., pick-up-task, close-task).",
				},
			},
			"required": []string{"dir_name", "phase"},
		},
		Permission: permReadOnly,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *phaseGuidanceTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	dirName, _ := args["dir_name"].(string)
	phaseName, _ := args["phase"].(string)
	if dirName == "" || phaseName == "" {
		return extsdk.Result{Error: "dir_name and phase are required"}
	}

	workflows := DiscoverWorkflows(t.listTool.userDir, t.listTool.projectDir)
	var wf *Workflow
	for _, w := range workflows {
		if w.DirName == dirName {
			wf = w
			break
		}
	}
	if wf == nil {
		return extsdk.Result{Error: fmt.Sprintf("workflow %q not found", dirName)}
	}

	guidance := PhaseGuidance(wf, phaseName)
	if guidance == "" {
		return extsdk.Result{Error: fmt.Sprintf("phase %q not found in workflow %q", phaseName, dirName)}
	}

	ts := EffectiveToolScope(wf, phaseName)
	if len(ts.Allow) > 0 || len(ts.Deny) > 0 {
		guidance += "\n\n**Effective tool scope for this phase:**\n"
		if len(ts.Allow) > 0 {
			guidance += fmt.Sprintf("- Allow: %s\n", strings.Join(ts.Allow, ", "))
		}
		if len(ts.Deny) > 0 {
			guidance += fmt.Sprintf("- Deny: %s\n", strings.Join(ts.Deny, ", "))
		}
	}

	return extsdk.Result{Success: true, Output: guidance}
}

// ── workflow_create ───────────────────────────────────────────────────

// createWorkflowTool scaffolds a new WORKFLOW.md in the user's workflow directory.
type createWorkflowTool struct {
	userDir string
}

func (t *createWorkflowTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name:        "workflow_create",
		Description: "Create a new WORKFLOW.md scaffold for a recurring task pattern. Workflows provide phase-keyed guidance that helps agents follow consistent processes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "A short, descriptive name for the workflow (e.g., 'code-review-checklist').",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "What this workflow helps accomplish.",
				},
				"when_to_use": map[string]interface{}{
					"type":        "string",
					"description": "When should the agent apply this workflow? (e.g., 'when reviewing a pull request')",
				},
			},
			"required": []string{"name", "description", "when_to_use"},
		},
		Permission: permWrite,
		HasEffects: true,
	}
}

func (t *createWorkflowTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	whenToUse, _ := args["when_to_use"].(string)

	if name == "" || description == "" || whenToUse == "" {
		return extsdk.Result{Error: "name, description, and when_to_use are required"}
	}

	dirName := sanitizeDirName(name)
	if dirName == "" {
		return extsdk.Result{Error: "workflow name produces an empty directory name after sanitization"}
	}
	dirPath := filepath.Join(t.userDir, dirName)

	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("create dir: %v", err)}
	}

	fm := frontmatterBlock{
		Name:        name,
		Description: description,
		WhenToUse:   whenToUse,
	}
	fmYAML, err := yaml.Marshal(fm)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("marshal frontmatter: %v", err)}
	}

	content := fmt.Sprintf("---\n%s---\n\n# %s\n\n%s\n\n## Phases\n\n", string(fmYAML), name, description)
	if err := os.WriteFile(filepath.Join(dirPath, workflowMD), []byte(content), 0644); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("write file: %v", err)}
	}

	return extsdk.Result{Success: true, Output: fmt.Sprintf("Created workflow %q at %s", name, dirPath)}
}

// ── workflow_match ────────────────────────────────────────────────────

// bestMatchTool finds the best workflow for a task description.
type bestMatchTool struct {
	listTool *listWorkflowsTool
}

func (t *bestMatchTool) def() extsdk.ToolDef {
	return extsdk.ToolDef{
		Name: "workflow_match",
		Description: "Find the best matching workflow for a given task description. " +
			"Returns the most relevant WORKFLOW.md that should be applied.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task": map[string]interface{}{
					"type":        "string",
					"description": "The task description to match against available workflows.",
				},
			},
			"required": []string{"task"},
		},
		Permission: permReadOnly,
		HasEffects: false,
		// Core: true (concurrent reads/writes are guarded by the runner).
		ConcurrencySafe: true,
	}
}

func (t *bestMatchTool) run(ctx context.Context, args map[string]interface{}) extsdk.Result {
	task, _ := args["task"].(string)
	if task == "" {
		return extsdk.Result{Error: "task is required"}
	}

	workflows := DiscoverWorkflows(t.listTool.userDir, t.listTool.projectDir)
	best := BestMatch(task, workflows)
	if best == nil {
		return extsdk.Result{Success: true, Output: "No matching workflow found for this task."}
	}

	b, err := json.Marshal(best.Summary())
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("marshal workflow summary: %v", err)}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

// safeNameRe matches characters safe for directory names.
var safeNameRe = regexp.MustCompile(`[^a-z0-9_-]`)

// sanitizeDirName converts a workflow name into a safe directory name,
// stripping all characters except lowercase alphanumerics, hyphens, and underscores.
// Returns an empty string if the result would be empty or starts with a dot.
func sanitizeDirName(name string) string {
	cleaned := safeNameRe.ReplaceAllString(strings.ToLower(name), "")
	// Collapse consecutive hyphens/underscores into a single hyphen.
	cleaned = regexp.MustCompile(`[-_]{2,}`).ReplaceAllString(cleaned, "-")
	// Trim leading/trailing hyphens and underscores.
	cleaned = strings.Trim(cleaned, "-_")
	if cleaned == "" || cleaned[0] == '.' {
		return ""
	}
	return cleaned
}
