package capability

import (
	"context"
	"encoding/json"
	"fmt"
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

			// Build if the binary doesn't exist and a build command is provided.
			// In dev mode, this auto-compiles Go extensions. In production,
			// pre-compiled binaries are embedded and no build is needed.
			if !binaryExists(binaryPath) && mf.Build != "" {
				log.Info("building extension", "name", mf.Name, "command", mf.Build)
				if err := buildExtension(extDir, mf.Build, log); err != nil {
					log.Warn("extension build failed", "name", mf.Name, "error", err)
					cleanPartialBinary(binaryPath)
					continue
				}
				binaryPath = resolveExtensionBinary(extDir, mf.Binary)
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
			for _, t := range extTools {
				reg.RegisterTool(setID, t)
			}

			// Register agents.
			extAgents, err := proc.ListAgents(context.Background())
			if err != nil {
				log.Warn("extension list agents failed", "name", mf.Name, "error", err)
			}
			for _, a := range extAgents {
				aCopy := a
				reg.RegisterAgent(setID, &aCopy)
			}

			reg.TrackExtension(setID, proc)

			if err := reg.AddSet(set); err != nil {
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

// buildExtension runs the build command inside the extension directory.
// Simple commands (no shell metacharacters) are executed directly for
// portability. Commands with pipes, redirects, or variable expansions use
// the platform shell. A timeout prevents stuck builds from blocking startup.
func buildExtension(extDir, buildCmd string, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	args, needsShell := parseBuildCommand(buildCmd)
	if len(args) == 0 {
		return fmt.Errorf("build command is empty or whitespace-only")
	}
	var cmd *exec.Cmd
	if needsShell {
		shell, shellArg := buildShell()
		cmd = exec.CommandContext(ctx, shell, shellArg, buildCmd)
	} else {
		cmd = exec.CommandContext(ctx, args[0], args[1:]...)
	}
	cmd.Dir = extDir
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Env = buildEnv(os.Environ())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build command failed: %w", err)
	}
	return nil
}

// parseBuildCommand splits a build command into argv. Returns needsShell=true
// when the command contains shell metacharacters (|, >, <, &, ;, $, `, *).
// Handles single-quoted and double-quoted strings.
func parseBuildCommand(cmd string) ([]string, bool) {
	var args []string
	var current strings.Builder
	inSingle, inDouble := false, false
	hasMeta := false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		// Handle backslash escape sequences inside quotes.
		if ch == '\\' && (inSingle || inDouble) && i+1 < len(cmd) {
			next := cmd[i+1]
			if next == '"' || next == '\'' || next == '\\' {
				current.WriteByte(next)
				i++
				continue
			}
		}
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case !inSingle && !inDouble && (ch == '|' || ch == '>' || ch == '<' ||
			ch == '&' || ch == ';' || ch == '$' || ch == '`' || ch == '*'):
			hasMeta = true
			current.WriteByte(ch)
		case ch == ' ' && !inSingle && !inDouble:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args, hasMeta
}

// buildShell returns the platform-appropriate shell and the flag that
// accepts a command string (usually "-c" on Unix, "/c" on Windows).
func buildShell() (shell, cmdFlag string) {
	if runtime.GOOS == "windows" {
		// Prefer PowerShell on Windows; fall back to cmd.
		if _, err := exec.LookPath("powershell"); err == nil {
			return "powershell", "-Command"
		}
		return "cmd", "/c"
	}
	return "sh", "-c"
}

// buildEnv returns the environment slice for extension builds. It clears
// GOFLAGS (which can interfere with go build flags) while preserving the
// module cache and toolchain paths. On Windows, GOPATH may be empty —
// that's fine; go build uses the default module cache location.
func buildEnv(parent []string) []string {
	env := make([]string, 0, len(parent)+3)
	for _, kv := range parent {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			continue // drop global GOFLAGS
		}
		env = append(env, kv)
	}
	// Re-add cleared vars with safe values.
	env = append(env, "GOFLAGS=")
	// Ensure toolchain env vars are present (even if empty — go build
	// uses defaults when they're unset).
	for _, key := range []string{"GOPATH", "GOROOT", "HOME", "USERPROFILE"} {
		if !hasEnvKey(parent, key) {
			if v := os.Getenv(key); v != "" {
				env = append(env, key+"="+v)
			}
		}
	}
	return env
}

// cleanPartialBinary removes a binary that may be a failed/partial build.
func cleanPartialBinary(path string) {
	os.Remove(path)
	if runtime.GOOS == "windows" || filepath.Ext(path) == "" {
		os.Remove(path + ".exe")
	}
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
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
