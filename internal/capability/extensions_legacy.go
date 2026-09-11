package capability

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// discoverLegacyExtensions scans the extensions directory for native binaries and scripts
// (Python, Node.js, Ruby, etc. — resolved via shebang or extension), launches
// each one, and registers their tools and agents into the registry.
func discoverLegacyExtensions(reg *CapabilityRegistry, legacyExtensionsDir string, loadCfg ExtensionConfigLoader, log *slog.Logger) (int, error) {
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
		// No on-disk manifest: identity comes from the process handshake.
		if _, err := launchExtensionProc(reg, extensionPath, nil, loadCfg, log); err != nil {
			log.Warn("extension start failed", "path", extensionPath, "error", err)
			continue
		}
		loaded++
	}

	return loaded, nil
}
