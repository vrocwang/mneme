package capability

import (
	"encoding/json"
	ws "github.com/simon/mneme/internal/workspace"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Bootstrap wires the capability sources that are NOT part of the builtin
// bundle: skills, extensions, and config-driven MCP servers. The builtin
// agents and core tools are registered by internal/bundle (see
// bundle.RegisterBuiltin), which the boot layer invokes before this.
func Bootstrap(reg *CapabilityRegistry, workspace string, mcpServers []ServerEntry, loadCfg ExtensionConfigLoader, log *slog.Logger) {
	// 1. Installed skills from skills/ directory.
	skillsDir := ws.SkillsDir(workspace)
	DiscoverSkills(reg, skillsDir, log)

	// 2. Extension modules from extensions/ directory (auto-build + load).
	//    Each extension has a manifest.json and is loaded via JSON-RPC.
	extensionDirs := DefaultExtensionDirs(workspace)
	discoverExtensions(reg, extensionDirs, loadCfg, log)

	// 3. MCP servers from config
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
		mdPath := filepath.Join(skillPath, ws.FileSkill)
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
//
// A skill's declared `tools` are the tools it *uses* (per the SKILL.md spec),
// not tools it provides: nothing registers an executable for them, so they are
// surfaced as descriptive metadata instead of ToolDescriptors. Listing them as
// callable tools would advertise names that GetTool/Execute cannot resolve.
func registerSkillSet(reg *CapabilityRegistry, m SkillManifest) error {
	cfg, _ := json.Marshal(m)
	slug := strings.ToLower(strings.ReplaceAll(m.Name, " ", "-"))

	desc := m.Description
	if len(m.Tools) > 0 {
		if desc != "" {
			desc += " "
		}
		desc += "Uses tools: " + strings.Join(m.Tools, ", ")
	}

	set := &CapabilitySet{
		ID:          SkillSetID(slug),
		Name:        m.Name,
		Kind:        KindSkill,
		Description: desc,
		Config:      cfg,
		Health:      HealthOK,
		Enabled:     true,
	}
	if err := reg.AddSet(set); err != nil {
		// Set already registered (e.g. skill re-installed). Replace it so
		// re-installation is idempotent and doesn't fail the agent turn.
		reg.replaceSet(set)
	}
	return nil
}
