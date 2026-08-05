package context

import (
	"strings"
)

// PromptTemplate provides variable substitution and conditional section rendering
// for system prompts. Agents can customize which sections appear and inject
// agent-specific content.
type PromptTemplate struct {
	// Variables available for template substitution.
	Variables map[string]string

	// OmittedSections contains section names that should be excluded from rendering.
	OmittedSections map[string]bool
}

// NewPromptTemplate creates a prompt template with the given variables.
func NewPromptTemplate(vars map[string]string) *PromptTemplate {
	return &PromptTemplate{
		Variables:       vars,
		OmittedSections: make(map[string]bool),
	}
}

// OmitSection marks a section as excluded from rendering.
func (pt *PromptTemplate) OmitSection(name string) *PromptTemplate {
	pt.OmittedSections[name] = true
	return pt
}

// IncludeSection ensures a section is included (removes omission flag).
func (pt *PromptTemplate) IncludeSection(name string) *PromptTemplate {
	delete(pt.OmittedSections, name)
	return pt
}

// Render substitutes variables in the given text using {{key}} syntax.
func (pt *PromptTemplate) Render(text string) string {
	if pt == nil || len(pt.Variables) == 0 {
		return text
	}

	result := text
	for key, value := range pt.Variables {
		result = strings.ReplaceAll(result, "{{"+key+"}}", value)
		result = strings.ReplaceAll(result, "{{ "+key+" }}", value)
	}
	return result
}

// ShouldOmit returns true if a section should be excluded.
func (pt *PromptTemplate) ShouldOmit(name string) bool {
	if pt == nil {
		return false
	}
	return pt.OmittedSections[name]
}

// ── Section omission flags ─────────────────────────────────────────────────

// SectionOmissionFlags defines common section categories that agents can toggle.
type SectionOmissionFlags struct {
	NoIdentity    bool // omit identity section
	NoDateTime    bool // omit date/time section
	NoSafety      bool // omit safety rules
	NoTools       bool // omit tool descriptions (e.g. for summarizer sub-agents)
	NoWorkspace   bool // omit workspace info
	NoSkills      bool // omit skills section
	NoPreferences bool // omit user preferences
	NoMemory      bool // omit memory context
	NoGuidance    bool // omit response guidelines
}

// ApplyToTemplate applies omission flags to a template.
func (f *SectionOmissionFlags) ApplyToTemplate(pt *PromptTemplate) {
	if f.NoIdentity {
		pt.OmitSection("identity")
	}
	if f.NoDateTime {
		pt.OmitSection("datetime")
	}
	if f.NoSafety {
		pt.OmitSection("safety")
	}
	if f.NoTools {
		pt.OmitSection("tools")
	}
	if f.NoWorkspace {
		pt.OmitSection("workspace")
	}
	if f.NoSkills {
		pt.OmitSection("skills")
	}
	if f.NoPreferences {
		pt.OmitSection("preferences")
	}
	if f.NoMemory {
		pt.OmitSection("memory")
	}
	if f.NoGuidance {
		pt.OmitSection("guidance")
	}
}

// DefaultSectionFlags returns flags with all sections enabled (no omissions).
func DefaultSectionFlags() *SectionOmissionFlags {
	return &SectionOmissionFlags{}
}

// SubAgentFlags returns flags suitable for sub-agents (minimal identity, no guidance).
func SubAgentFlags() *SectionOmissionFlags {
	return &SectionOmissionFlags{
		NoGuidance:    true,
		NoPreferences: true,
		NoSkills:      true,
	}
}

// SummarizerFlags returns flags for summarizer sub-agents (identity + tools only).
func SummarizerFlags() *SectionOmissionFlags {
	return &SectionOmissionFlags{
		NoDateTime:    true,
		NoSafety:      true,
		NoWorkspace:   true,
		NoSkills:      true,
		NoPreferences: true,
		NoMemory:      true,
		NoGuidance:    true,
	}
}

// WorkerFlags returns flags for worker sub-agents (minimal prompt).
func WorkerFlags() *SectionOmissionFlags {
	return &SectionOmissionFlags{
		NoSkills:      true,
		NoPreferences: true,
		NoMemory:      true,
		NoGuidance:    true,
	}
}

// ── Agent-scoped prompt building ──────────────────────────────────────────

// AgentPromptConfig controls how a system prompt is built for a specific agent.
type AgentPromptConfig struct {
	AgentName       string
	AgentRole       string   // short description of the agent's role
	OmittedSections []string // section names to omit
	ExtraContext    string   // additional context appended after all sections
	Variables       map[string]string
}

// BuildAgentPrompt renders a full system prompt with agent-specific configuration.
func (b *SystemPromptBuilder) BuildAgentPrompt(pctx *PromptContext, config *AgentPromptConfig) string {
	if config == nil {
		return b.Build(pctx)
	}

	pt := NewPromptTemplate(config.Variables)
	for _, name := range config.OmittedSections {
		pt.OmitSection(name)
	}

	var parts []string
	for _, s := range b.sections {
		if pt.ShouldOmit(s.Name()) {
			continue
		}
		text := s.Render(pctx)
		if text != "" {
			parts = append(parts, pt.Render(text))
		}
	}

	result := strings.Join(parts, "\n\n")

	// If agent role is specified, prepend it before the identity section.
	if config.AgentRole != "" {
		roleBlock := "You are acting as: " + config.AgentRole + "\nYour name: " + config.AgentName
		result = roleBlock + "\n\n" + result
	}

	// Append extra context.
	if config.ExtraContext != "" {
		result += "\n\n" + config.ExtraContext
	}

	return result
}
