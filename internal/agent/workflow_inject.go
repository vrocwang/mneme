package agent

import (
	"context"
	"strings"

	"github.com/simon/mneme/internal/agent_workflows"
)

// WorkflowInjector matches user tasks against available WORKFLOW.md files and
// injects relevant phase-keyed workflow guidance into the system prompt.
// Follows the same pattern as SkillsInjector for consistent prompt composition.
type WorkflowInjector struct {
	userDir    string
	projectDir string
	workflows  []*agent_workflows.Workflow
}

// NewWorkflowInjector creates an injector that discovers workflows from the
// given user and project directories.
func NewWorkflowInjector(userWorkflowsDir, projectRoot string) *WorkflowInjector {
	return &WorkflowInjector{
		userDir:    userWorkflowsDir,
		projectDir: projectRoot,
	}
}

// Load discovers all WORKFLOW.md files from the configured directories.
func (wi *WorkflowInjector) Load() {
	wi.workflows = agent_workflows.DiscoverWorkflows(wi.userDir, wi.projectDir)
}

// Match finds workflows relevant to the user's task description.
// Uses BestMatch (word-overlap scoring on the when_to_use field) and
// falls back to name/description matching.
func (wi *WorkflowInjector) Match(ctx context.Context, task string) []*agent_workflows.Workflow {
	if len(wi.workflows) == 0 {
		return nil
	}
	taskLower := strings.ToLower(task)

	// Score each workflow by word overlap with when_to_use, name, and description.
	type scored struct {
		wf    *agent_workflows.Workflow
		score int
	}
	var results []scored
	for _, wf := range wi.workflows {
		score := 0
		if whenToUse := wf.Frontmatter["when_to_use"]; whenToUse != "" {
			score += agent_workflows.WordOverlap(taskLower, strings.ToLower(whenToUse))
		}
		score += agent_workflows.WordOverlap(taskLower, strings.ToLower(wf.Name))
		score += agent_workflows.WordOverlap(taskLower, strings.ToLower(wf.Description))
		if score > 0 {
			results = append(results, scored{wf: wf, score: score})
		}
	}

	// Sort by score descending, cap at 3.
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	if len(results) > 3 {
		results = results[:3]
	}

	out := make([]*agent_workflows.Workflow, len(results))
	for i, r := range results {
		out[i] = r.wf
	}
	return out
}

// InjectPrompt appends matched workflow guidance to the system prompt.
// When a task matches a workflow, the agent receives phase-keyed instructions
// and tool scope constraints for structured task execution.
func (wi *WorkflowInjector) InjectPrompt(ctx context.Context, userMessage, systemPrompt string) string {
	matches := wi.Match(ctx, userMessage)
	if len(matches) == 0 {
		return systemPrompt
	}

	var b strings.Builder
	b.WriteString(systemPrompt)
	b.WriteString("\n\n## Available Workflows\n\n")
	b.WriteString("The following workflows are relevant to this task. Each workflow provides ")
	b.WriteString("phase-keyed guidance — follow the current phase's rules, respect tool scopes, ")
	b.WriteString("and advance phases when their conditions are met.\n\n")

	for _, wf := range matches {
		b.WriteString("### ")
		b.WriteString(wf.Name)
		b.WriteString("\n")
		if wf.Description != "" {
			b.WriteString(wf.Description)
			b.WriteString("\n\n")
		}
		if len(wf.Phases) > 0 {
			b.WriteString("**Phases:**\n")
			for _, name := range wf.PhaseNames() {
				guidance := agent_workflows.PhaseGuidance(wf, name)
				if guidance != "" {
					// Truncate per-phase guidance to save context.
					if len(guidance) > 800 {
						guidance = guidance[:800] + "...\n"
					}
					b.WriteString(guidance)
				}
			}
			b.WriteString("\n")
		}
		if len(wf.Tools.Allow) > 0 || len(wf.Tools.Deny) > 0 {
			b.WriteString("**Global tool scope:**\n")
			if len(wf.Tools.Allow) > 0 {
				b.WriteString("- Allow: " + strings.Join(wf.Tools.Allow, ", ") + "\n")
			}
			if len(wf.Tools.Deny) > 0 {
				b.WriteString("- Deny: " + strings.Join(wf.Tools.Deny, ", ") + "\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Len returns the number of loaded workflows.
func (wi *WorkflowInjector) Len() int {
	return len(wi.workflows)
}

// Workflows returns the currently loaded workflows (read-only).
func (wi *WorkflowInjector) Workflows() []*agent_workflows.Workflow {
	return wi.workflows
}
