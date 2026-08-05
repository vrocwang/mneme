package capability

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/internal/tools"
)

// discoverLegacyExtensions scans the extensions directory for native binaries and scripts
// (Python, Node.js, Ruby, etc. — resolved via shebang or extension), launches
// each one, and registers their tools and agents into the registry.
func discoverLegacyExtensions(reg *CapabilityRegistry, legacyExtensionsDir string, log *slog.Logger) (int, error) {
	if _, err := os.Stat(legacyExtensionsDir); os.IsNotExist(err) {
		log.Info("extensions directory does not exist, skipping", "dir", legacyExtensionsDir)
		return 0, nil
	}

	entries, err := os.ReadDir(legacyExtensionsDir)
	if err != nil {
		return 0, err
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		extensionPath := filepath.Join(legacyExtensionsDir, entry.Name())
		proc, err := tools.StartProtoFromCommand(context.Background(), extensionPath, log)
		if err != nil {
			log.Warn("extension start failed", "path", extensionPath, "error", err)
			continue
		}

		setID := "extension:" + proc.Manifest.Name
		set := &CapabilitySet{
			ID:          setID,
			Name:        proc.Manifest.Name,
			Kind:        KindExtension,
			Description: proc.Manifest.Description,
			Health:      HealthOK,
			Enabled:     true,
		}

		// Register tools.
		extTools, err := proc.ListTools(context.Background())
		if err != nil {
			log.Warn("extension list tools failed", "extension", proc.Manifest.Name, "error", err)
		}
		for _, t := range extTools {
			reg.RegisterTool(setID, t)
		}

		// Register agents.
		extAgents, err := proc.ListAgents(context.Background())
		if err != nil {
			log.Warn("extension list agents failed", "extension", proc.Manifest.Name, "error", err)
		}
		for _, a := range extAgents {
			aCopy := a
			reg.RegisterAgent(setID, &aCopy)
		}

		// Track the process for lifecycle management.
		reg.TrackExtension(setID, proc)

		if err := reg.AddSet(set); err != nil {
			log.Warn("extension set registration failed", "name", proc.Manifest.Name, "error", err)
			proc.Stop()
			continue
		}
		loaded++
		log.Info("extension loaded", "name", proc.Manifest.Name, "tools", len(extTools), "agents", len(extAgents))
	}

	return loaded, nil
}
