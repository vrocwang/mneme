// Package prompts manages configurable LLM prompt templates. Defaults are embedded
// in the binary; at runtime, prompts are loaded from the user's workspace (filesystem),
// falling back to the embedded defaults when no override file exists.
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed defaults/*.txt
var embedded embed.FS

// Name identifies a prompt template.
type Name string

// NameFromAgentID maps an agent ID to its corresponding prompt name.
func NameFromAgentID(agentID string) Name {
	return Name("agent_" + agentID)
}

// All returns every embedded prompt name, sorted.
func All() []Name {
	entries, err := fs.ReadDir(embedded, "defaults")
	if err != nil {
		return nil
	}
	names := make([]Name, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		names = append(names, Name(strings.TrimSuffix(e.Name(), ".txt")))
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
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

// List returns metadata for all built-in prompts plus any custom prompts
// found as .txt files in the workspace prompts directory.
func (m *Manager) List() []PromptMeta {
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
				if seen[name] {
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

// Description returns a human-readable explanation of what the prompt
// controls, derived from its name: agent prompts describe their agent;
// everything else falls back to an empty description.
func (n Name) Description() string {
	s := string(n)
	if id, ok := strings.CutPrefix(s, "agent_"); ok && id != "" {
		return "System prompt for the " + strings.ReplaceAll(id, "_", " ") + " agent."
	}
	return ""
}

func (m *Manager) pathFor(name Name) string {
	return filepath.Join(m.dir, string(name)+".txt")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
