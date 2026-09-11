package agent

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/simon/mneme/internal/workspace"
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
type SkillsInjector struct {
	skillsDir string
	skills    []SkillMatch // loaded filesystem skills content
}

// NewSkillsInjector creates an injector that loads skills from the given directory.
func NewSkillsInjector(skillsDir string) *SkillsInjector {
	return &SkillsInjector{skillsDir: skillsDir}
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
			skillPath := filepath.Join(si.skillsDir, entry.Name(), workspace.FileSkill)
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

	// Best matches first, then cap at 3.
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	if len(matches) > 3 {
		matches = matches[:3]
	}
	return matches
}

// InjectPrompt appends matched skill content to the system prompt.
func (si *SkillsInjector) InjectPrompt(ctx context.Context, userMessage, systemPrompt string) string {
	matches := si.Match(ctx, userMessage)
	if len(matches) == 0 {
		return systemPrompt
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
	return b.String()
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
