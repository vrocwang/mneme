package agent_workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/simon/mneme/internal/capability"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/simon/mneme/internal/tools"
)

// ListWorkflowsTool lists available workflows.
type ListWorkflowsTool struct {
	userDir    string
	projectDir string
	cache      []*Workflow
}

// NewListWorkflowsTool creates a workflow listing tool.
func NewListWorkflowsTool(userWorkflowsDir, projectRoot string) *ListWorkflowsTool {
	return &ListWorkflowsTool{userDir: userWorkflowsDir, projectDir: projectRoot}
}

func (t *ListWorkflowsTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "workflow_list",
		Description: "List available WORKFLOW.md workflows that provide phase-keyed guidance for complex tasks. Use this to discover task-specific workflows before starting work.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *ListWorkflowsTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	workflows := DiscoverWorkflows(t.userDir, t.projectDir)
	t.cache = workflows

	if len(workflows) == 0 {
		return tools.Result{Success: true, Output: "No workflows found."}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Available workflows (%d):\n\n", len(workflows)))
	for _, w := range workflows {
		b.WriteString(fmt.Sprintf("- **%s** (%s)\n", w.Name, w.DirName))
		if w.Description != "" {
			b.WriteString(fmt.Sprintf("  %s\n", w.Description))
		}
		if len(w.Phases) > 0 {
			b.WriteString(fmt.Sprintf("  Phases: %s\n", strings.Join(w.PhaseNames(), ", ")))
		}
		if len(w.Tags) > 0 {
			b.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(w.Tags, ", ")))
		}
		b.WriteString("\n")
	}
	return tools.Result{Success: true, Output: b.String()}
}

// ReadWorkflowTool reads a specific workflow by directory name.
type ReadWorkflowTool struct {
	listTool *ListWorkflowsTool
}

// NewReadWorkflowTool creates a workflow read tool.
func NewReadWorkflowTool(listTool *ListWorkflowsTool) *ReadWorkflowTool {
	return &ReadWorkflowTool{listTool: listTool}
}

func (t *ReadWorkflowTool) Schema() tools.Schema {
	return tools.Schema{
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
	}
}

func (t *ReadWorkflowTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	dirName, _ := args["dir_name"].(string)
	if dirName == "" {
		return tools.Result{Error: "dir_name is required"}
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
		return tools.Result{Error: fmt.Sprintf("workflow %q not found", dirName)}
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
	return tools.Result{Success: true, Output: b.String()}
}

// PhaseGuidanceTool resolves phase-specific guidance and tool scope.
type PhaseGuidanceTool struct {
	listTool *ListWorkflowsTool
}

// NewPhaseGuidanceTool creates a phase guidance tool.
func NewPhaseGuidanceTool(listTool *ListWorkflowsTool) *PhaseGuidanceTool {
	return &PhaseGuidanceTool{listTool: listTool}
}

func (t *PhaseGuidanceTool) Schema() tools.Schema {
	return tools.Schema{
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
	}
}

func (t *PhaseGuidanceTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	dirName, _ := args["dir_name"].(string)
	phaseName, _ := args["phase"].(string)
	if dirName == "" || phaseName == "" {
		return tools.Result{Error: "dir_name and phase are required"}
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
		return tools.Result{Error: fmt.Sprintf("workflow %q not found", dirName)}
	}

	guidance := PhaseGuidance(wf, phaseName)
	if guidance == "" {
		return tools.Result{Error: fmt.Sprintf("phase %q not found in workflow %q", phaseName, dirName)}
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

	return tools.Result{Success: true, Output: guidance}
}

// CreateWorkflowTool scaffolds a new WORKFLOW.md in the user's workflow directory.
type CreateWorkflowTool struct {
	userDir string
}

// NewCreateWorkflowTool creates a workflow scaffold tool.
func NewCreateWorkflowTool(userWorkflowsDir string) *CreateWorkflowTool {
	return &CreateWorkflowTool{userDir: userWorkflowsDir}
}

func (t *CreateWorkflowTool) Schema() tools.Schema {
	return tools.Schema{
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
	}
}

func (t *CreateWorkflowTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	whenToUse, _ := args["when_to_use"].(string)

	if name == "" || description == "" || whenToUse == "" {
		return tools.Result{Error: "name, description, and when_to_use are required"}
	}

	dirName := sanitizeDirName(name)
	if dirName == "" {
		return tools.Result{Error: "workflow name produces an empty directory name after sanitization"}
	}
	dirPath := filepath.Join(t.userDir, dirName)

	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return tools.Result{Error: fmt.Sprintf("create dir: %v", err)}
	}

	fm := frontmatterBlock{
		Name:        name,
		Description: description,
		WhenToUse:   whenToUse,
	}
	fmYAML, err := yaml.Marshal(fm)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal frontmatter: %v", err)}
	}

	content := fmt.Sprintf("---\n%s---\n\n# %s\n\n%s\n\n## Phases\n\n", string(fmYAML), name, description)
	if err := os.WriteFile(filepath.Join(dirPath, workflowMD), []byte(content), 0644); err != nil {
		return tools.Result{Error: fmt.Sprintf("write file: %v", err)}
	}

	return tools.Result{Success: true, Output: fmt.Sprintf("Created workflow %q at %s", name, dirPath)}
}

// BestMatchTool finds the best workflow for a task description.
type BestMatchTool struct {
	listTool *ListWorkflowsTool
}

// NewBestMatchTool creates a workflow matching tool.
func NewBestMatchTool(listTool *ListWorkflowsTool) *BestMatchTool {
	return &BestMatchTool{listTool: listTool}
}

func (t *BestMatchTool) Schema() tools.Schema {
	return tools.Schema{
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
	}
}

func (t *BestMatchTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	task, _ := args["task"].(string)
	if task == "" {
		return tools.Result{Error: "task is required"}
	}

	workflows := DiscoverWorkflows(t.listTool.userDir, t.listTool.projectDir)
	best := BestMatch(task, workflows)
	if best == nil {
		return tools.Result{Success: true, Output: "No matching workflow found for this task."}
	}

	b, err := json.Marshal(best.Summary())
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal workflow summary: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
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

// RegisterTools registers all agent workflow tools with the capability registry.
func RegisterTools(reg *capability.CapabilityRegistry, userWorkflowsDir, projectRoot string) {
	listTool := NewListWorkflowsTool(userWorkflowsDir, projectRoot)
	reg.RegisterTool("builtin", listTool)
	reg.RegisterTool("builtin", NewReadWorkflowTool(listTool))
	reg.RegisterTool("builtin", NewPhaseGuidanceTool(listTool))
	reg.RegisterTool("builtin", NewCreateWorkflowTool(userWorkflowsDir))
	reg.RegisterTool("builtin", NewBestMatchTool(listTool))
}
