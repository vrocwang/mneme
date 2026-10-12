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
//
// disabled is the configured [bundles] disabled id list, passed as plain
// strings so this package does not import internal/bundle. It gates process
// extensions only; internal/bundle.Registry remains the single reader of that
// list for in-process bundles.
//
// opts carries the extension loading policy (lazy declaration, idle reaping,
// cache directory); see ExtensionLoadOptions. When opts.Lazy is set, the idle
// reaper starts here and is owned by the registry (cancelled by Shutdown).
func Bootstrap(reg *CapabilityRegistry, workspace string, mcpServers []ServerEntry, loadCfg ExtensionConfigLoader, disabled []string, opts ExtensionLoadOptions, log *slog.Logger) {
	// 1. Installed skills from skills/ directory.
	skillsDir := ws.SkillsDir(workspace)
	DiscoverSkills(reg, skillsDir, log)

	// 2. Extension modules from extensions/ directory (auto-build + load).
	//    Each extension has a manifest.json and is loaded via JSON-RPC.
	//    Extensions named (or set-id named) in [bundles] disabled are skipped
	//    before their build, and the filter is published on the registry so the
	//    install-time path can warn about an extension config disables.
	extensionDirs := DefaultExtensionDirs(workspace)
	filter := NewExtensionFilter(disabled)
	reg.SetExtensionFilter(filter)
	discoverExtensions(reg, extensionDirs, loadCfg, filter, opts, log)

	// 3. Idle reaper for lazily started extensions. Only lazy loading creates
	//    reaped processes, so a non-lazy boot starts no goroutine.
	if opts.Lazy {
		reg.startIdleReaper(opts.IdleTimeout)
	}

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
