package capability

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/simon/mneme/internal/tools"
)

// Extension manifest type is defined in the tools package to avoid
// duplication between capability discovery and tools lifecycle management.
// See tools.ExtensionManifest for the canonical definition.

// discoverExtensions scans one or more extensions directories for subdirectories
// containing a manifest.json, optionally builds each one, launches it as an extension
// subprocess, and registers its tools and agents into the registry.
//
// Directories are tried in order. If a directory doesn't exist, it is silently
// skipped. This allows the same code to work in development (project-root
// extensions/) and production (workspace extensions/).
func discoverExtensions(reg *CapabilityRegistry, dirs []string, log *slog.Logger) (int, error) {
	loaded := 0

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Warn("extensions directory read failed", "dir", dir, "error", err)
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			extDir := filepath.Join(dir, entry.Name())
			mfPath := filepath.Join(extDir, "manifest.json")

			mfData, err := os.ReadFile(mfPath)
			if err != nil {
				continue // no manifest, not an extension
			}

			var mf tools.ExtensionManifest
			if err := json.Unmarshal(mfData, &mf); err != nil {
				log.Warn("extension manifest parse failed", "dir", extDir, "error", err)
				continue
			}
			if err := mf.Validate(); err != nil {
				log.Warn("extension manifest invalid", "dir", extDir, "error", err)
				continue
			}

			log.Info("extension discovered", "name", mf.Name, "version", mf.Version, "category", mf.Category)

			// Determine the binary path. The binary field may be a simple name
			// (relative to the extension dir) or an interpreter command.
			binaryPath := resolveExtensionBinary(extDir, mf.Binary)

			// Runtime discovery loads pre-built binaries. In a development
			// source tree (the extension dir contains a go.mod and a build
			// command), we auto-build once so `wails dev` works without a
			// separate pre-compile step. Production binaries are already
			// present, so the build is never triggered there.
			if !binaryExists(binaryPath) {
				if mf.Build != "" && fileExists(filepath.Join(extDir, "go.mod")) {
					log.Info("building extension from source", "name", mf.Name, "command", mf.Build)
					if err := buildExtension(extDir, mf.Build); err != nil {
						log.Warn("extension build failed", "name", mf.Name, "error", err)
						continue
					}
					binaryPath = resolveExtensionBinary(extDir, mf.Binary)
				} else {
					if mf.Build != "" {
						log.Warn("extension binary not found; build it via cmd/build-extensions before loading",
							"name", mf.Name, "binary", binaryPath)
					} else {
						log.Warn("extension binary not found", "name", mf.Name, "binary", binaryPath)
					}
				}
			}

			if !binaryExists(binaryPath) {
				log.Warn("extension binary not found", "name", mf.Name, "binary", binaryPath)
				continue
			}

			// Launch the extension via the JSON-RPC protocol (JSON-RPC over stdio).
			// Use StartProtoFromCommand which handles native binaries, scripts,
			// with shebangs, and interpreter invocations.
			proc, err := tools.StartProtoFromCommand(context.Background(), binaryPath, log)
			if err != nil {
				errStr := err.Error()
				// Windows UAC elevation requests and missing interpreters are
				// expected for some extensions — log at info level.
				if strings.Contains(errStr, "requires elevation") ||
					strings.Contains(errStr, "exec format error") ||
					strings.Contains(errStr, "no such file") {
					log.Info("extension skipped", "name", mf.Name, "reason", errStr)
				} else {
					log.Warn("extension start failed", "name", mf.Name, "binary", binaryPath, "error", err)
				}
				continue
			}

			setID := "extension:" + mf.Name
			set := &CapabilitySet{
				ID:          setID,
				Name:        mf.Name,
				Kind:        KindExtension,
				Description: mf.Description,
				Health:      HealthOK,
				Enabled:     true,
			}

			// Register tools.
			extTools, err := proc.ListTools(context.Background())
			if err != nil {
				log.Warn("extension list tools failed", "name", mf.Name, "error", err)
			}

			// Register agents.
			extAgents, err := proc.ListAgents(context.Background())
			if err != nil {
				log.Warn("extension list agents failed", "name", mf.Name, "error", err)
			}
			agentDefs := make([]*tools.AgentDef, 0, len(extAgents))
			for _, a := range extAgents {
				aCopy := a
				agentDefs = append(agentDefs, &aCopy)
			}

			// Register the extension as a single effect. The returned dispose is
			// stored in the registry and unwound on shutdown/uninstall.
			if _, err := reg.RegisterExtension(setID, set, proc, extTools, agentDefs); err != nil {
				// Set already registered - same extension found in a different
				// directory. Stop the duplicate process and skip silently.
				proc.Stop()
				continue
			}
			loaded++
			log.Info("extension loaded", "name", mf.Name, "tools", len(extTools), "agents", len(extAgents))
		}
	}

	return loaded, nil
}

// resolveExtensionBinary resolves the binary path from the manifest's binary field.
// If the binary field is a multi-word command (e.g. "python3 script.py"), return
// it as-is. If it's a simple name, resolve relative to the extension directory.
// On Windows, appends .exe when binary has no extension (Go builds produce .exe).
func resolveExtensionBinary(extDir, binary string) string {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return ""
	}
	// Multi-word: interpreter + script (e.g. "python3 myskill.py").
	if strings.Contains(binary, " ") {
		parts := strings.Fields(binary)
		// Resolve the last argument (the script) relative to the extension dir.
		script := parts[len(parts)-1]
		if !filepath.IsAbs(script) {
			parts[len(parts)-1] = filepath.Join(extDir, script)
		}
		return strings.Join(parts, " ")
	}
	// Simple name or path.
	var result string
	if filepath.IsAbs(binary) {
		result = binary
	} else {
		result = filepath.Join(extDir, binary)
	}
	// On Windows: append .exe if the binary has no extension and the .exe file exists.
	if runtime.GOOS == "windows" && filepath.Ext(result) == "" {
		if _, err := os.Stat(result + ".exe"); err == nil {
			return result + ".exe"
		}
	}
	return result
}

// binaryExists returns true if the path refers to an existing regular file.
// For interpreter commands (containing spaces), checks the script file.
// On Windows, also checks path.exe if the path has no extension.
func binaryExists(path string) bool {
	if path == "" {
		return false
	}
	// For "python3 script.py", check script.py.
	if strings.Contains(path, " ") {
		parts := strings.Fields(path)
		path = parts[len(parts)-1]
	}
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return true
	}
	// On Windows, Go builds produce .exe files.
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		info, err := os.Stat(path + ".exe")
		return err == nil && !info.IsDir()
	}
	return false
}

// fileExists reports whether the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// buildExtension runs the extension's build command inside its directory. It
// is only invoked for development source trees (go.mod present) where the
// binary is missing; production never reaches this path because binaries are
// pre-built and present. The command is executed through the platform shell so
// quoted flags (e.g. -ldflags="-s -w") are parsed correctly.
func buildExtension(extDir, buildCmd string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", buildCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", buildCmd)
	}
	cmd.Dir = extDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// DefaultExtensionDirs returns the standard extension search paths for the
// given workspace directory. Development-time extensions/ directory is
// resolved relative to the current working directory and the binary location.
func DefaultExtensionDirs(workspace string) []string {
	// Priority 1: workspace/extensions (production - extracted from embed).
	wsExt := filepath.Join(workspace, "extensions")
	if entries, err := os.ReadDir(wsExt); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				if _, err := os.Stat(filepath.Join(wsExt, e.Name(), "manifest.json")); err == nil {
					return []string{wsExt} // workspace has extensions, use it
				}
			}
		}
	}

	// Priority 2: cwd/extensions (development - project source tree).
	if cwd, err := os.Getwd(); err == nil {
		return []string{filepath.Join(cwd, "extensions")}
	}

	// Priority 3: exeDir/extensions (standalone binary).
	if exe, err := os.Executable(); err == nil {
		return []string{filepath.Join(filepath.Dir(exe), "extensions")}
	}

	return []string{wsExt}
}
