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

		// Collect tools and agents up front so the whole extension can be
		// registered as a single effect (RegisterExtension reserves the set ID
		// first and returns a dispose func for clean unwinding).
		var extTools []tools.Tool
		if ts, err := proc.ListTools(context.Background()); err == nil {
			extTools = ts
		} else {
			log.Warn("extension list tools failed", "extension", proc.Manifest.Name, "error", err)
		}
		var extAgents []*tools.AgentDef
		if as, err := proc.ListAgents(context.Background()); err == nil {
			for _, a := range as {
				aCopy := a
				extAgents = append(extAgents, &aCopy)
			}
		} else {
			log.Warn("extension list agents failed", "extension", proc.Manifest.Name, "error", err)
		}

		// Register as a single effect. On a duplicate set ID RegisterExtension
		// fails closed without mutating state; on success the process is tracked
		// for Shutdown/dispose and the dispose is stored in the registry.
		if _, err := reg.RegisterExtension(setID, set, proc, extTools, extAgents); err != nil {
			log.Warn("extension set registration failed", "name", proc.Manifest.Name, "error", err)
			proc.Stop()
			continue
		}
		loaded++
		log.Info("extension loaded", "name", proc.Manifest.Name, "tools", len(extTools), "agents", len(extAgents))
	}

	return loaded, nil
}
