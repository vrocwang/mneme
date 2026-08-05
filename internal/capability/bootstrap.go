package capability

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/internal/config"
)

// Bootstrap wires all capability sources into the registry.
// app.go calls only this — no registration logic in the Wails proxy.
func Bootstrap(reg *CapabilityRegistry, workspace string, securityTier string, mcpServers []ServerEntry, braveAPIKey, tavilyAPIKey, searxngURL string, proxyConfig config.ProxyConfig, cfg *config.Config, log *slog.Logger) {
	// 1. Core tools + agents (builtins)
	registerBuiltins(reg, workspace, securityTier, braveAPIKey, tavilyAPIKey, searxngURL, proxyConfig, cfg, log)

	// 2. Installed skills from skills/ directory.
	skillsDir := filepath.Join(workspace, "skills")
	DiscoverSkills(reg, skillsDir, log)

	// 3. Extension modules from extensions/ directory (auto-build + load).
	//    Each extension has a manifest.json and is loaded via JSON-RPC.
	extensionDirs := DefaultExtensionDirs(workspace)
	discoverExtensions(reg, extensionDirs, log)

	// 4. MCP servers from config
	for _, srv := range mcpServers {
		connectMCPServer(reg, srv, log)
	}

	log.Info("capability bootstrap complete", "sets", len(reg.ListSets()))
}

// DiscoverSkills scans the skills directory and registers each subdirectory
// containing a valid SKILL.md as a KindSkill capability set.
func DiscoverSkills(reg *CapabilityRegistry, skillsDir string, log *slog.Logger) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return // directory doesn't exist or can't be read, nothing to do
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name())
		mdPath := filepath.Join(skillPath, "SKILL.md")
		data, err := os.ReadFile(mdPath)
		if err != nil {
			continue
		}
		m, _, err := parseSkillFrontmatter(data)
		if err != nil || m.Name == "" {
			log.Warn("invalid SKILL.md, skipping", "dir", skillPath, "error", err)
			continue
		}
		if err := registerSkillSet(reg, *m); err != nil {
			log.Warn("failed to register skill", "name", m.Name, "error", err)
			continue
		}
	}
}

// registerSkillSet creates a CapabilitySet from a skill manifest and adds it
// to the registry.
func registerSkillSet(reg *CapabilityRegistry, m SkillManifest) error {
	cfg, _ := json.Marshal(m)
	slug := strings.ToLower(strings.ReplaceAll(m.Name, " ", "-"))

	toolDescs := make([]ToolDescriptor, 0, len(m.Tools))
	for _, t := range m.Tools {
		toolDescs = append(toolDescs, ToolDescriptor{
			Name:        t,
			Description: "Skill tool: " + m.Name + " — see SKILL.md for implementation",
			Permission:  "read_only",
		})
	}

	desc := m.Description
	if len(toolDescs) > 0 && desc == "" {
		desc = "Provides " + strings.Join(m.Tools, ", ")
	}

	set := &CapabilitySet{
		ID:          "skill:" + slug,
		Name:        m.Name,
		Kind:        KindSkill,
		Description: desc,
		Tools:       toolDescs,
		ToolCount:   len(toolDescs),
		Config:      cfg,
		Health:      HealthOK,
		Enabled:     true,
	}
	if err := reg.AddSet(set); err != nil {
		// Set already registered (e.g. skill re-installed). Update in place so
		// re-installation is idempotent and doesn't fail the agent turn.
		reg.mu.Lock()
		reg.sets[set.ID] = set
		reg.mu.Unlock()
	}
	return nil
}
