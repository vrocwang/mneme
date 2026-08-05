package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelSpec specifies how an agent resolves its model.
type ModelSpec struct {
	Type string `json:"type"`           // "inherit", "hint", "exact"
	Hint string `json:"hint,omitempty"` // workload hint: "agentic", "chat", "summarization"
	Name string `json:"name,omitempty"` // exact model name
}

// ToolScope defines which tools an agent can access.
type ToolScope struct {
	Type  string   `json:"type"` // "all", "named", "allowed_all"
	Named []string `json:"named,omitempty"`
}

// SandboxMode restricts sub-agent filesystem access.
type SandboxMode string

const (
	SandboxNone      SandboxMode = "none"
	SandboxReadOnly  SandboxMode = "read_only"
	SandboxReadWrite SandboxMode = "read_write"
)

// IterationPolicy controls max iterations behavior.
type IterationPolicy string

const (
	IterPolicyStandard IterationPolicy = "standard" // 10 iterations
	IterPolicyExtended IterationPolicy = "extended" // 50 iterations (code/integration agents)
)

// AgentTier enforces spawn hierarchy: Chat > Reasoning > Worker.
type AgentTier string

const (
	TierChat      AgentTier = "chat"
	TierReasoning AgentTier = "reasoning"
	TierWorker    AgentTier = "worker"
)

// PromptSource defines where the system prompt comes from.
type PromptSource struct {
	Type string `json:"type"`           // "inline", "file", "dynamic"
	Path string `json:"path,omitempty"` // file path for "file" type
}

// AgentProfile is a user-selectable agent personality persisted to disk.
type AgentProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Which built-in agent definition to use as the base
	AgentID string `json:"agent_id"`

	// Overrides
	ModelOverride string  `json:"model_override,omitempty"`
	Temperature   float64 `json:"temperature,omitempty"`
	PromptSuffix  string  `json:"system_prompt_suffix,omitempty"`

	// Tool configuration
	AllowedTools []string `json:"allowed_tools,omitempty"`

	// Personality
	SoulMD     string `json:"soul_md,omitempty"`
	SoulMDPath string `json:"soul_md_path,omitempty"`

	// Presentation
	AvatarURL string `json:"avatar_url,omitempty"`
	VoiceID   string `json:"voice_id,omitempty"`

	// Memory isolation — distinct memory directories per personality
	MemoryDirSuffix string `json:"memory_dir_suffix,omitempty"`

	// Integration gating
	ComposioIntegrations []string `json:"composio_integrations,omitempty"`

	// Metadata
	IsMaster  bool   `json:"is_master"`
	BuiltIn   bool   `json:"built_in"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// AgentProfileStore manages persisted agent profiles.
type AgentProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]*AgentProfile
	dir      string
	selected string // active profile ID
}

// NewAgentProfileStore creates a profile store backed by the given directory.
func NewAgentProfileStore(dir string) (*AgentProfileStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create profiles dir: %w", err)
	}

	s := &AgentProfileStore{
		profiles: make(map[string]*AgentProfile),
		dir:      dir,
		selected: "default",
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("load profiles: %w", err)
	}

	// Ensure default profile exists
	if _, ok := s.profiles["default"]; !ok {
		s.profiles["default"] = &AgentProfile{
			ID:          "default",
			Name:        "Default",
			Description: "Default agent profile",
			AgentID:     "general",
			IsMaster:    true,
			BuiltIn:     true,
			SortOrder:   0,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		s.saveOne(s.profiles["default"])
	}

	return s, nil
}

// load reads all profile files from disk.
func (s *AgentProfileStore) load() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p AgentProfile
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		if p.ID == "" {
			continue
		}
		s.profiles[p.ID] = &p
	}

	// Read selected profile
	statePath := filepath.Join(s.dir, ".selected")
	if data, err := os.ReadFile(statePath); err == nil {
		s.selected = strings.TrimSpace(string(data))
	}

	return nil
}

// saveOne writes a single profile to disk atomically via tempfile+rename.
func (s *AgentProfileStore) saveOne(p *AgentProfile) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, p.ID+".json")
	tmp, err := os.CreateTemp(s.dir, ".profile-*.tmp")
	if err != nil {
		return fmt.Errorf("create tempfile for profile %s: %w", p.ID, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("write tempfile for profile %s: %w", p.ID, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("fsync tempfile for profile %s: %w", p.ID, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("close tempfile for profile %s: %w", p.ID, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("rename tempfile for profile %s: %w", p.ID, err)
	}
	return nil
}

// Save persists all profiles and selected state.
func (s *AgentProfileStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.profiles {
		if err := s.saveOne(p); err != nil {
			return err
		}
	}

	// Save selected atomically
	return s.atomicWriteSelected(s.selected)
}

// Get returns a profile by ID.
func (s *AgentProfileStore) Get(id string) *AgentProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profiles[id]
}

// List returns all profiles sorted by sort_order.
func (s *AgentProfileStore) List() []*AgentProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*AgentProfile
	for _, p := range s.profiles {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].SortOrder < result[j].SortOrder
	})
	return result
}

// Upsert creates or updates a profile, normalizing the ID.
func (s *AgentProfileStore) Upsert(p *AgentProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	if p.ID == "" {
		p.ID = slugify(p.Name)
	}
	p.ID = slugify(p.ID)

	existing, ok := s.profiles[p.ID]
	if ok {
		p.CreatedAt = existing.CreatedAt
	} else {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	// Auto-assign memory dir suffix if not set.
	// Re-upsert of an existing profile without a suffix reuses the stored suffix.
	// New profiles get the lowest unused numbered suffix ("-1", "-2", …).
	if p.MemoryDirSuffix == "" {
		if existing, ok := s.profiles[p.ID]; ok && existing.MemoryDirSuffix != "" {
			p.MemoryDirSuffix = existing.MemoryDirSuffix
		} else {
			p.MemoryDirSuffix = nextAvailableSuffix(s.profiles, p.ID)
		}
	}

	s.profiles[p.ID] = p
	return s.saveOne(p)
}

// Delete removes a profile. Cannot delete the selected or default profile.
func (s *AgentProfileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "default" {
		return fmt.Errorf("cannot delete default profile")
	}
	if id == s.selected {
		return fmt.Errorf("cannot delete active profile; switch profiles first")
	}

	delete(s.profiles, id)
	path := filepath.Join(s.dir, id+".json")
	os.Remove(path)
	return nil
}

// atomicWriteSelected persists the selected profile ID to disk atomically.
func (s *AgentProfileStore) atomicWriteSelected(id string) error {
	statePath := filepath.Join(s.dir, ".selected")
	tmp, err := os.CreateTemp(s.dir, ".selected-*.tmp")
	if err != nil {
		return fmt.Errorf("create tempfile for selected state: %w", err)
	}
	if _, err := tmp.Write([]byte(id)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("write tempfile for selected state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("fsync tempfile for selected state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("close tempfile for selected state: %w", err)
	}
	return os.Rename(tmp.Name(), statePath)
}

// Select changes the active profile. Returns the profile.
func (s *AgentProfileStore) Select(id string) (*AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.profiles[id]
	if !ok {
		return nil, fmt.Errorf("profile %q not found", id)
	}

	s.selected = id
	if err := s.atomicWriteSelected(id); err != nil {
		return p, err
	}
	return p, nil
}

// Selected returns the currently active profile.
func (s *AgentProfileStore) Selected() *AgentProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.profiles[s.selected]
	if !ok {
		return s.profiles["default"]
	}
	return p
}

// SelectedID returns the ID of the active profile.
func (s *AgentProfileStore) SelectedID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selected
}

// BuildPromptSection generates the profile section for system prompt injection.
func (s *AgentProfileStore) BuildPromptSection() string {
	profile := s.Selected()
	if profile == nil || profile.ID == "default" {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n## Agent Profile\n")
	b.WriteString(fmt.Sprintf("You are operating as **%s**.", profile.Name))
	if profile.Description != "" {
		b.WriteString(fmt.Sprintf(" %s", profile.Description))
	}
	b.WriteString("\n")

	if profile.SoulMD != "" {
		b.WriteString("### Personality\n")
		b.WriteString(profile.SoulMD)
		b.WriteString("\n")
	}

	return b.String()
}

// nextAvailableSuffix returns the lowest unused numbered suffix ("-1", "-2", …)
// not present among the existing profiles, excluding the profile being upserted.
func nextAvailableSuffix(profiles map[string]*AgentProfile, excludeID string) string {
	used := make(map[string]bool)
	for id, p := range profiles {
		if id != excludeID && p.MemoryDirSuffix != "" {
			used[p.MemoryDirSuffix] = true
		}
	}
	for n := 1; n <= 999; n++ {
		candidate := fmt.Sprintf("-%d", n)
		if !used[candidate] {
			return candidate
		}
	}
	// Fallback — should never happen with a reasonable number of profiles.
	return fmt.Sprintf("-%d", time.Now().UnixNano())
}

// slugify converts a name to a safe ID.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	result = strings.Trim(result, "-")
	if result == "" {
		return "profile"
	}
	return result
}
