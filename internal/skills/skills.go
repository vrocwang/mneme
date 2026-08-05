package skills

// Skill represents a registered skill with metadata.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	// ToolDescriptors are injected into agent prompts.
	ToolDescriptors []ToolDescriptor `json:"toolDescriptors,omitempty"`
}

// ToolDescriptor describes a tool provided by this skill.
type ToolDescriptor struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Registry holds installed skills.
type Registry struct {
	skills map[string]*Skill
}

func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

// Register adds a skill.
func (r *Registry) Register(skill *Skill) {
	r.skills[skill.Name] = skill
}

// Get returns a skill by name.
func (r *Registry) Get(name string) *Skill {
	return r.skills[name]
}

// List returns all registered skills.
func (r *Registry) List() []*Skill {
	result := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		result = append(result, s)
	}
	return result
}

// AllToolDescriptors returns all tool descriptors from all skills.
func (r *Registry) AllToolDescriptors() []ToolDescriptor {
	var result []ToolDescriptor
	for _, skill := range r.skills {
		result = append(result, skill.ToolDescriptors...)
	}
	return result
}

// PromptInjection builds the system prompt section for skills.
func (r *Registry) PromptInjection() string {
	skills := r.List()
	if len(skills) == 0 {
		return ""
	}

	var out string
	out += "## Available Skills\n\n"
	for _, s := range skills {
		out += "### " + s.Name + " (v" + s.Version + ")\n"
		out += s.Description + "\n"
		for _, td := range s.ToolDescriptors {
			out += "- Tool: `" + td.Name + "` — " + td.Description + "\n"
		}
		out += "\n"
	}
	return out
}
