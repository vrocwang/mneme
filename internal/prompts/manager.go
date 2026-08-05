// Package prompts manages configurable LLM prompt templates. Defaults are embedded
// in the binary; at runtime, prompts are loaded from the user's workspace (filesystem),
// falling back to the embedded defaults when no override file exists.
package prompts

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed defaults/*.txt
var embedded embed.FS

// Name identifies a prompt template.
type Name string

const (
	SystemIdentity       Name = "system_identity"
	SystemSafety         Name = "system_safety"
	SystemGuidance       Name = "system_guidance"
	AgentGeneral         Name = "agent_general"
	AgentCoder           Name = "agent_coder"
	AgentArchivist       Name = "agent_archivist"
	AgentOrchestrator    Name = "agent_orchestrator"
	AgentResearcher      Name = "agent_researcher"
	AgentCritic          Name = "agent_critic"
	AgentPlanner         Name = "agent_planner"
	AgentSummarizer      Name = "agent_summarizer"
	AgentHelp            Name = "agent_help"
	AgentToolsAgent      Name = "agent_tools_agent"
	AgentTriggerReactor  Name = "agent_trigger_reactor"
	SummarizerToolOutput Name = "summarizer_tool_output"
	ArchivistMemory      Name = "archivist_memory"
	CouncilChair         Name = "council_chair"
	NudgeMemoryGaps      Name = "nudge_memory_gaps"
	NudgeIdle            Name = "nudge_idle"
)

// All returns every known prompt name.
func All() []Name {
	return []Name{
		SystemIdentity, SystemSafety, SystemGuidance,
		AgentGeneral, AgentCoder, AgentArchivist,
		AgentOrchestrator, AgentResearcher, AgentCritic,
		AgentPlanner, AgentSummarizer, AgentHelp,
		AgentToolsAgent, AgentTriggerReactor,
		SummarizerToolOutput, ArchivistMemory, CouncilChair,
		NudgeMemoryGaps, NudgeIdle,
	}
}

// NameFromAgentID maps an agent ID to its corresponding prompt name.
func NameFromAgentID(agentID string) Name {
	return Name("agent_" + agentID)
}

// Manager loads prompts from a workspace directory, falling back to embedded defaults.
type Manager struct {
	dir string // workspace subdirectory for prompt overrides
}

// New creates a Manager rooted at the given workspace prompt directory.
func New(workspaceDir string) *Manager {
	return &Manager{dir: filepath.Join(workspaceDir, "prompts")}
}

// Get returns the prompt text for the given name.
func (m *Manager) Get(name Name) string {
	if m.dir != "" {
		path := m.pathFor(name)
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}
	embedPath := "defaults/" + string(name) + ".txt"
	data, err := embedded.ReadFile(embedPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// GetDefault returns the embedded default text for a built-in prompt,
// ignoring any filesystem override. For custom prompts, returns "".
func (m *Manager) GetDefault(name Name) string {
	embedPath := "defaults/" + string(name) + ".txt"
	data, err := embedded.ReadFile(embedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[prompts] GetDefault(%s): embedded.ReadFile(%q) failed: %v\n", name, embedPath, err)
		return ""
	}
	return string(data)
}

// Set writes an override prompt to the workspace. An empty body removes the
// override (reverts to embedded default). Returns the new effective prompt.
func (m *Manager) Set(name Name, body string) error {
	if m.dir == "" {
		return fmt.Errorf("prompt workspace not configured")
	}
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return err
	}
	path := m.pathFor(name)
	body = strings.TrimSpace(body)
	if body == "" {
		os.Remove(path)
		return nil
	}
	return os.WriteFile(path, []byte(body), 0644)
}

// builtinNames returns a set of all built-in prompt names.
func builtinNames() map[string]bool {
	set := make(map[string]bool)
	for _, n := range All() {
		set[string(n)] = true
	}
	return set
}

// List returns metadata for all built-in prompts plus any custom prompts
// found as .txt files in the workspace prompts directory.
func (m *Manager) List() []PromptMeta {
	builtins := builtinNames()
	seen := make(map[string]bool)
	var out []PromptMeta

	// Built-in prompts first.
	for _, name := range All() {
		s := string(name)
		seen[s] = true
		effective := m.Get(name)
		embedPath := "defaults/" + s + ".txt"
		defaultData, err := embedded.ReadFile(embedPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[prompts] List: embedded.ReadFile(%q) failed: %v\n", embedPath, err)
		}
		out = append(out, PromptMeta{
			Name:        s,
			Description: name.Description(),
			Length:      len(effective),
			Overridden:  m.dir != "" && fileExists(m.pathFor(name)),
			DefaultLen:  len(string(defaultData)),
			Builtin:     true,
		})
	}

	// Custom prompts from the filesystem.
	if m.dir != "" {
		entries, err := os.ReadDir(m.dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if !strings.HasSuffix(name, ".txt") {
					continue
				}
				name = strings.TrimSuffix(name, ".txt")
				if seen[name] || builtins[name] {
					continue
				}
				seen[name] = true
				data, _ := os.ReadFile(m.pathFor(Name(name)))
				out = append(out, PromptMeta{
					Name:    name,
					Length:  len(data),
					Builtin: false,
				})
			}
		}
	}

	return out
}

// Delete removes a prompt override file. For built-in prompts this reverts
// to the default. For custom prompts this removes the prompt entirely.
func (m *Manager) Delete(name Name) error {
	if m.dir == "" {
		return fmt.Errorf("prompt workspace not configured")
	}
	return os.Remove(m.pathFor(name))
}

// PromptMeta describes a prompt for listing in a settings UI.
type PromptMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Length      int    `json:"length"`
	Overridden  bool   `json:"overridden"`
	DefaultLen  int    `json:"default_length"`
	Builtin     bool   `json:"builtin"`
}

// Description returns a human-readable explanation of what each prompt controls.
// These are i18n keys on the frontend; the Go side ships them as display-ready
// English text so the settings page works even without a frontend build.
func (n Name) Description() string {
	switch n {
	case SystemIdentity:
		return "Core identity and role description injected at the top of every system prompt."
	case SystemSafety:
		return "Hard safety rules — destructive commands, data loss prevention, credential handling."
	case SystemGuidance:
		return "Tone, verbosity, and response-style instructions applied to every agent turn."
	case AgentGeneral:
		return "Base system prompt for the general-purpose orchestrator agent."
	case AgentCoder:
		return "System prompt for the code-executor specialist sub-agent."
	case AgentArchivist:
		return "System prompt for the memory archivist sub-agent."
	case AgentOrchestrator:
		return "System prompt for the orchestrator sub-agent that coordinates specialists."
	case AgentResearcher:
		return "System prompt for the research specialist sub-agent."
	case AgentCritic:
		return "System prompt for the critic sub-agent that reviews outputs."
	case AgentPlanner:
		return "System prompt for the planner sub-agent that breaks down tasks."
	case AgentSummarizer:
		return "System prompt for the summarizer sub-agent that condenses information."
	case AgentHelp:
		return "System prompt for the help sub-agent that answers product questions."
	case AgentToolsAgent:
		return "System prompt for the tools/integration specialist sub-agent."
	case AgentTriggerReactor:
		return "System prompt for the trigger reactor sub-agent that handles automated events."
	case SummarizerToolOutput:
		return "Instructions for condensing tool outputs before they enter the conversation."
	case ArchivistMemory:
		return "Curation prompt for extracting long-term memories from conversations."
	case CouncilChair:
		return "Synthesis prompt used by the chair model in multi-model council deliberation."
	case NudgeMemoryGaps:
		return "Template for subconscious nudges about under-represented memory areas."
	case NudgeIdle:
		return "Template for idle-reminder nudges after extended inactivity."
	default:
		return ""
	}
}

func (m *Manager) pathFor(name Name) string {
	return filepath.Join(m.dir, string(name)+".txt")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
