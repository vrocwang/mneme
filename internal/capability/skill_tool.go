package capability

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/pkg/tools"
	"gopkg.in/yaml.v3"
)

// NewSkillInstallTool creates an agent-callable tool that installs a skill
// from a URL (including raw GitHub URLs) or a local SKILL.md file path.
func NewSkillInstallTool(skillsDir string, reg *CapabilityRegistry) tools.Tool {
	return &skillInstallTool{skillsDir: skillsDir, reg: reg}
}

type skillInstallTool struct {
	skillsDir string
	reg       *CapabilityRegistry
}

func (t *skillInstallTool) Schema() tools.Schema {
	return tools.Schema{
		Name: "skill_install",
		Description: "Install a skill from a URL (GitHub raw or any HTTP URL) or local file path. " +
			"The source must point to a SKILL.md file with YAML frontmatter containing at least a 'name' field. " +
			"Plain markdown without frontmatter is also accepted — metadata is auto-generated from the content. " +
			"GitHub blob URLs are automatically rewritten to raw URLs. " +
			"The installed skill will appear in the Skills capability tab and its instructions are injected into future conversations.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source": map[string]interface{}{
					"type":        "string",
					"description": "URL or local path to a SKILL.md file. Supports http/https URLs and local file paths.",
				},
			},
			"required": []string{"source"},
		},
	}
}

func (t *skillInstallTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	source, _ := args["source"].(string)
	if source == "" {
		return tools.Result{Error: "source is required — provide a URL or local path to a SKILL.md file"}
	}

	if t.skillsDir == "" {
		return tools.Result{Error: "skills directory not configured"}
	}

	var data []byte
	var err error
	if isURL(source) {
		data, err = downloadSkillURL(source)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("read skill: %v", err)}
	}

	m, body, err := parseSkillFrontmatter(data)
	if err != nil {
		// Plain markdown without YAML frontmatter — auto-generate one.
		m = extractManifestFromMarkdown(data, source)
		body = string(data)
	}
	if m.Name == "" {
		return tools.Result{Error: "could not determine skill name from content"}
	}

	skillDir := filepath.Join(t.skillsDir, m.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return tools.Result{Error: fmt.Sprintf("create skill dir: %v", err)}
	}

	// Write the SKILL.md with proper YAML frontmatter.
	fullContent, err := yaml.Marshal(m)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal manifest: %v", err)}
	}
	written := fmt.Sprintf("---\n%s---\n%s", string(fullContent), body)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(written), 0644); err != nil {
		return tools.Result{Error: fmt.Sprintf("write SKILL.md: %v", err)}
	}

	// Register the newly installed skill so it appears in the capabilities UI.
	if t.reg != nil {
		if err := registerSkillSet(t.reg, *m); err != nil {
			return tools.Result{Error: fmt.Sprintf("register skill: %v", err)}
		}
	}

	return tools.Result{
		Success: true,
		Output:  fmt.Sprintf("Skill %q installed successfully from %s. %d tool(s) declared.", m.Name, source, len(m.Tools)),
	}
}

// extractManifestFromMarkdown extracts a SkillManifest from plain markdown
// that lacks YAML frontmatter. The first # heading becomes the skill name
// (lowercased, spaces → dashes). The first non-heading paragraph becomes
// the description.
func extractManifestFromMarkdown(data []byte, source string) *SkillManifest {
	text := string(data)
	name := "unnamed-skill"
	desc := ""

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			heading := strings.TrimPrefix(trimmed, "# ")
			heading = strings.TrimLeft(heading, "0123456789. ")
			heading = strings.TrimLeft(heading, "— ")
			if name == "unnamed-skill" {
				name = strings.ToLower(strings.ReplaceAll(heading, " ", "-"))
			}
			if desc == "" {
				desc = heading
			}
		} else if desc == "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "```") {
			desc = trimmed
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}
		}
	}

	if name == "unnamed-skill" && source != "" {
		// Derive a unique name from the source path/URL when no heading found.
		base := source
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		base = strings.TrimSuffix(base, ".md")
		base = strings.TrimSuffix(base, ".txt")
		if base != "" {
			name = strings.ToLower(strings.ReplaceAll(base, " ", "-"))
		}
	}

	if desc == "" {
		desc = name
	}

	return &SkillManifest{
		Name:        name,
		Version:     "1.0",
		Description: desc,
		Homepage:    source,
	}
}
