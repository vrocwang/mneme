package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/internal/skills"
)

// SkillMatch represents a skill that matched a user query.
type SkillMatch struct {
	Name    string
	Path    string
	Content string
	Score   float64
}

// SkillsInjector matches user queries against installed SKILL.md files and
// injects relevant skill content into the system prompt to guide the agent.
// When a skills.Registry is set, registered skills are merged with file-based
// matches so both discovery paths contribute to prompt injection.
type SkillsInjector struct {
	skillsDir string
	skills    []SkillMatch     // loaded filesystem skills content
	registry  *skills.Registry // optional — registered skills from extensions/MCP
}

// NewSkillsInjector creates an injector that loads skills from the given directory.
func NewSkillsInjector(skillsDir string) *SkillsInjector {
	return &SkillsInjector{skillsDir: skillsDir}
}

// WithRegistry sets an optional skills registry. Registered skills are merged
// with file-based matches during prompt injection.
func (si *SkillsInjector) WithRegistry(reg *skills.Registry) *SkillsInjector {
	si.registry = reg
	return si
}

// Registry returns the skills registry, or nil if none is set.
// Callers (boot, extensions, MCP) use this to register skills dynamically.
func (si *SkillsInjector) Registry() *skills.Registry {
	return si.registry
}

// Load reads all SKILL.md files from the skills directory.
func (si *SkillsInjector) Load() error {
	if si.skillsDir == "" {
		return nil
	}

	entries, err := os.ReadDir(si.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	si.skills = nil
	for _, entry := range entries {
		if entry.IsDir() {
			skillPath := filepath.Join(si.skillsDir, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}
			si.skills = append(si.skills, SkillMatch{
				Name:    entry.Name(),
				Path:    skillPath,
				Content: string(data),
			})
		}
	}
	return nil
}

// Match finds skills relevant to the user's message.
func (si *SkillsInjector) Match(ctx context.Context, userMessage string) []SkillMatch {
	if len(si.skills) == 0 {
		return nil
	}

	lower := strings.ToLower(userMessage)
	var matches []SkillMatch
	for _, s := range si.skills {
		score := matchSkillScore(lower, s)
		if score > 0 {
			s.Score = score
			matches = append(matches, s)
		}
	}

	// Sort by score descending, cap at 3 matches.
	if len(matches) > 3 {
		matches = matches[:3]
	}
	return matches
}

// InjectPrompt appends matched skill content to the system prompt.
// Merges file-based SKILL.md matches with skills registered in the registry.
func (si *SkillsInjector) InjectPrompt(ctx context.Context, userMessage, systemPrompt string) string {
	matches := si.Match(ctx, userMessage)
	registrySkills := si.registrySkills()

	if len(matches) == 0 && len(registrySkills) == 0 {
		return systemPrompt
	}

	// Deduplicate: file-based matches take precedence over registry entries
	// with the same name (case-insensitive).
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		seen[strings.ToLower(m.Name)] = true
	}

	var b strings.Builder
	b.WriteString(systemPrompt)
	b.WriteString("\n\n## Relevant Skills\n\n")
	b.WriteString("The following skills are relevant to this request. Follow their instructions when applicable:\n\n")

	for _, m := range matches {
		content := m.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		b.WriteString("### " + m.Name + "\n")
		b.WriteString(content + "\n\n")
	}
	for _, s := range registrySkills {
		if seen[strings.ToLower(s.Name)] {
			continue
		}
		b.WriteString("### " + s.Name + "\n")
		b.WriteString(s.Description + "\n\n")
	}
	return b.String()
}

// registrySkills returns skills from the registry. Returns nil if no registry is set.
func (si *SkillsInjector) registrySkills() []*skills.Skill {
	if si.registry == nil {
		return nil
	}
	return si.registry.List()
}

// matchSkillScore computes how well a skill matches the user message.
func matchSkillScore(userLower string, skill SkillMatch) float64 {
	lowerName := strings.ToLower(skill.Name)
	lowerContent := strings.ToLower(skill.Content)

	// Name match is strongest signal.
	if strings.Contains(userLower, lowerName) {
		return 2.0
	}

	// Check for keyword overlap between message and skill content.
	userWords := strings.Fields(userLower)
	skillWords := strings.Fields(lowerContent)

	matchCount := 0
	for _, uw := range userWords {
		if len(uw) < 4 {
			continue // skip short words
		}
		for _, sw := range skillWords {
			if uw == sw {
				matchCount++
				break
			}
		}
	}

	if matchCount > 0 {
		return float64(matchCount) / float64(len(userWords))
	}
	return 0
}
