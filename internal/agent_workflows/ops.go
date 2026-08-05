package agent_workflows

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const workflowMD = "WORKFLOW.md"

// frontmatterBlock represents the YAML frontmatter in a WORKFLOW.md file.
type frontmatterBlock struct {
	Name        string   `yaml:"name,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	WhenToUse   string   `yaml:"when_to_use,omitempty"`
	Tools       struct {
		Allow []string `yaml:"allow,omitempty"`
		Deny  []string `yaml:"deny,omitempty"`
	} `yaml:"tools,omitempty"`
	Phases map[string]struct {
		Description string   `yaml:"description,omitempty"`
		Rules       string   `yaml:"rules,omitempty"`
		Scripts     []string `yaml:"scripts,omitempty"`
		Tools       struct {
			Allow []string `yaml:"allow,omitempty"`
			Deny  []string `yaml:"deny,omitempty"`
		} `yaml:"tools,omitempty"`
		Context string `yaml:"context,omitempty"`
	} `yaml:"phases,omitempty"`
}

// DiscoverWorkflows scans the user and project workflow directories for
// WORKFLOW.md files and returns parsed Workflow objects.
func DiscoverWorkflows(userWorkflowsDir, projectRoot string) []*Workflow {
	var out []*Workflow

	// User-scoped workflows (<workspace>/workflows/)
	if userWorkflowsDir != "" {
		entries, err := os.ReadDir(userWorkflowsDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				mdPath := filepath.Join(userWorkflowsDir, e.Name(), workflowMD)
				if w, err := parseWorkflowMD(mdPath); err == nil {
					w.DirName = e.Name()
					w.Scope = ScopeUser
					w.Discovered = time.Now()
					out = append(out, w)
				}
			}
		}
	}

	// Project-scoped workflows (<project>/.workflows/)
	if projectRoot != "" {
		projectDir := filepath.Join(projectRoot, ".workflows")
		entries, err := os.ReadDir(projectDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				mdPath := filepath.Join(projectDir, e.Name(), workflowMD)
				if w, err := parseWorkflowMD(mdPath); err == nil {
					w.DirName = e.Name()
					w.Scope = ScopeProject
					w.Discovered = time.Now()
					out = append(out, w)
				}
			}
		}
	}

	return out
}

// parseWorkflowMD reads and parses a WORKFLOW.md file.
func parseWorkflowMD(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseWorkflowBytes(data)
}

// parseWorkflowBytes parses WORKFLOW.md content from bytes.
func parseWorkflowBytes(data []byte) (*Workflow, error) {
	content := string(data)

	// Extract YAML frontmatter between --- markers.
	fm, body := extractFrontmatter(content)
	if fm == "" {
		return nil, fmt.Errorf("no frontmatter found")
	}

	var block frontmatterBlock
	if err := yaml.Unmarshal([]byte(fm), &block); err != nil {
		return nil, fmt.Errorf("invalid frontmatter: %w", err)
	}

	w := &Workflow{
		Name:        block.Name,
		Description: block.Description,
		Tags:        block.Tags,
		Tools: ToolScope{
			Allow: block.Tools.Allow,
			Deny:  block.Tools.Deny,
		},
		Phases: make(map[string]Phase),
		Frontmatter: map[string]string{
			"when_to_use": block.WhenToUse,
		},
	}

	// If name is empty, use the first heading from the body.
	if w.Name == "" && body != "" {
		w.Name = extractFirstHeading(body)
	}

	// Parse phases.
	for name, p := range block.Phases {
		phase := Phase{
			Name:        name,
			Description: p.Description,
			Rules:       p.Rules,
			Scripts:     p.Scripts,
			Context:     p.Context,
			Tools: ToolScope{
				Allow: p.Tools.Allow,
				Deny:  p.Tools.Deny,
			},
		}
		// Include rules from body sections if not in frontmatter.
		if phase.Rules == "" && body != "" {
			phase.Rules = extractPhaseRules(body, name)
		}
		w.Phases[name] = phase
	}

	return w, nil
}

// extractFrontmatter splits YAML frontmatter from markdown body.
// Frontmatter is delimited by --- lines.
func extractFrontmatter(content string) (frontmatter, body string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return "", content
	}
	if strings.TrimSpace(lines[0]) != "---" {
		return "", content
	}
	var endIdx int
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx == 0 {
		return "", content
	}
	fm := strings.Join(lines[1:endIdx], "\n")
	bodyLines := lines[endIdx+1:]
	body = strings.Join(bodyLines, "\n")
	return fm, body
}

// extractFirstHeading returns the text of the first markdown heading.
func extractFirstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
	}
	return ""
}

// extractPhaseRules extracts a phase's rules section from the markdown body.
func extractPhaseRules(body, phaseName string) string {
	// Look for "## Phase: <name>" or "## <name>" sections.
	prefixes := []string{
		"## Phase: " + phaseName,
		"## " + phaseName,
		"### Phase: " + phaseName,
		"### " + phaseName,
	}
	var capture bool
	var sb strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, p := range prefixes {
			if trimmed == p {
				capture = true
				break
			}
		}
		if capture && strings.HasPrefix(trimmed, "##") {
			// Next section starts — stop capturing.
			break
		}
		if capture {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return strings.TrimSpace(sb.String())
}

// PhaseGuidance renders a phase's rules as a markdown guidance block.
func PhaseGuidance(w *Workflow, phaseName string) string {
	phase, ok := w.Phases[phaseName]
	if !ok {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Workflow Phase: %s — %s\n\n", phaseName, phase.Description))
	if phase.Rules != "" {
		sb.WriteString(phase.Rules)
		sb.WriteString("\n\n")
	}
	if len(phase.Scripts) > 0 {
		sb.WriteString("**Scripts:**\n")
		for _, s := range phase.Scripts {
			sb.WriteString(fmt.Sprintf("- `%s`\n", s))
		}
		sb.WriteString("\n")
	}
	if len(phase.Tools.Allow) > 0 || len(phase.Tools.Deny) > 0 {
		sb.WriteString("**Tool scope for this phase:**\n")
		if len(phase.Tools.Allow) > 0 {
			sb.WriteString(fmt.Sprintf("- Allow: %s\n", strings.Join(phase.Tools.Allow, ", ")))
		}
		if len(phase.Tools.Deny) > 0 {
			sb.WriteString(fmt.Sprintf("- Deny: %s\n", strings.Join(phase.Tools.Deny, ", ")))
		}
	}
	return sb.String()
}

// EffectiveToolScope returns the union of workflow-level and phase-level tool scopes.
func EffectiveToolScope(w *Workflow, phaseName string) ToolScope {
	ts := ToolScope{
		Allow: append([]string{}, w.Tools.Allow...),
		Deny:  append([]string{}, w.Tools.Deny...),
	}
	if phase, ok := w.Phases[phaseName]; ok {
		ts.Allow = append(ts.Allow, phase.Tools.Allow...)
		ts.Deny = append(ts.Deny, phase.Tools.Deny...)
	}
	return ts
}

// BestMatch finds the workflow whose when_to_use field has the highest
// word-overlap with the task description.
func BestMatch(task string, workflows []*Workflow) *Workflow {
	var best *Workflow
	bestScore := 0
	taskLower := strings.ToLower(task)
	for _, w := range workflows {
		whenToUse := strings.ToLower(w.Frontmatter["when_to_use"])
		if whenToUse == "" {
			continue
		}
		score := WordOverlap(taskLower, whenToUse)
		if score > bestScore {
			bestScore = score
			best = w
		}
	}
	return best
}

// WordOverlap counts how many words from a appear in b.
func WordOverlap(a, b string) int {
	aWords := strings.Fields(a)
	bWords := make(map[string]bool)
	for _, w := range strings.Fields(b) {
		bWords[w] = true
	}
	count := 0
	for _, w := range aWords {
		if bWords[w] {
			count++
		}
	}
	return count
}
