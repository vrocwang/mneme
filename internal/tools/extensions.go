package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Layout constants ───────────────────────────────────────────────────

const (
	ExtensionsDirName     = "extensions"
	ExtensionManifestFile = "manifest.json"
)

// ── Category ────────────────────────────────────────────────────────────

// ExtensionCategory groups extensions by function.
type ExtensionCategory string

const (
	ExtCategoryChannel     ExtensionCategory = "channels"
	ExtCategoryIntegration ExtensionCategory = "integrations"
	ExtCategoryComposio    ExtensionCategory = "composio"
	ExtCategoryMemorySync  ExtensionCategory = "memory-sync"
	ExtCategoryDesktop     ExtensionCategory = "desktop"
	ExtCategoryAgent       ExtensionCategory = "agents"
)

// ── Manifest ────────────────────────────────────────────────────────────

// ExtensionManifest is the JSON manifest shipped with every extension.
type ExtensionManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Category    string `json:"category"`
	Description string `json:"description,omitempty"`
	Binary      string `json:"binary,omitempty"`
	// Build is an optional shell command to compile the extension before
	// loading. Run from the extension's directory. If empty, the binary
	// is assumed to be pre-built or a script that needs no compilation.
	Build    string `json:"build,omitempty"`
	Author   string `json:"author,omitempty"`
	Homepage string `json:"homepage,omitempty"`
	License  string `json:"license,omitempty"`

	// Frontend integration points.
	Routes      []string `json:"routes,omitempty"`
	ReduxSlice  string   `json:"redux_slice,omitempty"`
	SettingsTab string   `json:"settings_tab,omitempty"`
	I18nFiles   []string `json:"i18n,omitempty"`

	// Tool declarations.
	Tools       []string `json:"tools,omitempty"`
	Permissions []string `json:"permissions,omitempty"`

	// ConfigSchema is a JSON Schema describing the extension's configuration
	// options. The host uses this to render a dynamic settings form and
	// passes the user's values via extension.configure at startup.
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty"`
}

// Validate checks required fields.
func (m *ExtensionManifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest missing required field 'name'")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest missing required field 'version'")
	}
	if m.Category == "" {
		return fmt.Errorf("manifest missing required field 'category'")
	}
	if strings.ContainsAny(m.Name, "/\\") {
		return fmt.Errorf("manifest name contains path separator: %q", m.Name)
	}
	return nil
}

// ParseManifest reads and validates a manifest.json file.
func ParseManifest(path string) (*ExtensionManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m ExtensionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest JSON: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// ── Discovery ───────────────────────────────────────────────────────────

// DiscoveredExtension is a manifest paired with its installation path.
type DiscoveredExtension struct {
	Manifest    ExtensionManifest `json:"manifest"`
	InstallPath string            `json:"install_path"`
	BinaryPath  string            `json:"binary_path,omitempty"`
	Enabled     bool              `json:"enabled"`
	Health      string            `json:"health"`
	LoadedAt    *time.Time        `json:"loaded_at,omitempty"`
}

// ExtensionDiscovery scans the extensions directory and discovers installed
// extensions by reading their manifest.json files. The capability package
// uses ExtensionManifest directly (via this package) for startup discovery.
// ExtensionDiscovery and ExtensionProcess provide the runtime management API
// for install, uninstall, and lifecycle operations exposed via the frontend.
type ExtensionDiscovery struct {
	baseDir string // parent of the extensions/ directory (workspace root)
}

// NewExtensionDiscovery creates an extension discovery service.
// baseDir is typically the workspace root; extensions live under <baseDir>/extensions/.
func NewExtensionDiscovery(baseDir string) *ExtensionDiscovery {
	return &ExtensionDiscovery{baseDir: baseDir}
}

// ExtensionsDir returns the root extensions directory.
func (d *ExtensionDiscovery) ExtensionsDir() string {
	return filepath.Join(d.baseDir, ExtensionsDirName)
}

// Discover scans and returns all discovered extensions sorted by name.
func (d *ExtensionDiscovery) Discover() ([]DiscoveredExtension, error) {
	extDir := d.ExtensionsDir()
	if _, err := os.Stat(extDir); os.IsNotExist(err) {
		return nil, nil
	}

	var result []DiscoveredExtension

	err := filepath.Walk(extDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() || path == extDir {
			return nil
		}

		manifestPath := filepath.Join(path, ExtensionManifestFile)
		if _, mfErr := os.Stat(manifestPath); os.IsNotExist(mfErr) {
			return nil
		}

		m, parseErr := ParseManifest(manifestPath)
		if parseErr != nil {
			return nil
		}

		ext := DiscoveredExtension{
			Manifest:    *m,
			InstallPath: path,
			Enabled:     true,
			Health:      "unknown",
		}
		if m.Binary != "" {
			ext.BinaryPath = filepath.Join(path, m.Binary)
		}
		result = append(result, ext)
		return filepath.SkipDir
	})

	if err != nil {
		return nil, fmt.Errorf("extension discovery: %w", err)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Manifest.Name < result[j].Manifest.Name
	})
	return result, nil
}

// ResolveBinary returns the full path to an extension's binary.
func (d *ExtensionDiscovery) ResolveBinary(manifest ExtensionManifest) string {
	return filepath.Join(d.ExtensionsDir(), manifest.Category, manifest.Name, manifest.Binary)
}

// AutoBuild ensures the extension binary exists. If the binary is missing
// and a build command is declared, it runs the command in the extension's
// directory. Returns the resolved binary path, or an error if the binary
// could not be produced.
func (d *ExtensionDiscovery) AutoBuild(ext *DiscoveredExtension) (string, error) {
	mf := ext.Manifest
	if mf.Binary == "" {
		return "", fmt.Errorf("extension %q has no binary", mf.Name)
	}

	binPath := filepath.Join(ext.InstallPath, mf.Binary)

	// Already built — return as-is.
	if _, err := os.Stat(binPath); err == nil {
		ext.BinaryPath = binPath
		return binPath, nil
	}

	// Scripts (Python, Node, etc.) launched via interpreter — the
	// binary field IS the command string, not a file path. Check if
	// the binary field contains a known interpreter prefix.
	if isInterpreterCommand(mf.Binary) {
		ext.BinaryPath = mf.Binary
		return mf.Binary, nil
	}

	// No build command → hope the binary appears (pre-built).
	if mf.Build == "" {
		return "", fmt.Errorf("binary %q not found and no build command declared", mf.Binary)
	}

	// Run the build command.
	cmd := execCommand(mf.Build)
	cmd.Dir = ext.InstallPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build failed for %q: %w\n%s", mf.Name, err, string(output))
	}

	// Verify the binary now exists.
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("build succeeded but binary %q not found", binPath)
	}

	ext.BinaryPath = binPath
	return binPath, nil
}

// isInterpreterCommand returns true when the binary field is an
// interpreter invocation (e.g. "python3 script.py") rather than a
// standalone executable path.
func isInterpreterCommand(binary string) bool {
	parts := strings.Fields(binary)
	if len(parts) <= 1 {
		return false
	}
	first := filepath.Base(parts[0])
	switch first {
	case "python", "python3", "node", "ruby", "perl", "php", "lua", "bash", "sh", "npx":
		return true
	}
	return false
}

// execCommand splits a shell command string into args. Uses a platform-
// specific shell wrapper on Windows; direct exec on Unix.
func execCommand(cmdStr string) *exec.Cmd {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return exec.Command("")
	}
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", append([]string{"/c"}, parts...)...)
	}
	return exec.Command(parts[0], parts[1:]...)
}

// ── Category helpers ────────────────────────────────────────────────────

// InferCategory derives the extension category from its path relative to the
// extensions root directory.
func InferCategory(extPath, extRoot string) ExtensionCategory {
	rel, err := filepath.Rel(extRoot, extPath)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 3)
	if len(parts) >= 2 {
		return ExtensionCategory(parts[0])
	}
	return ""
}

// ExtensionDir returns the expected installation path for an extension.
func ExtensionDir(workspaceDir string, category ExtensionCategory, name string) string {
	return filepath.Join(workspaceDir, ExtensionsDirName, string(category), name)
}

// ── Extension Process ───────────────────────────────────────────────────

// ExtensionProcess manages a running extension backend process.
type ExtensionProcess struct {
	Manifest  ExtensionManifest
	Cmd       *exec.Cmd
	StartTime time.Time
	mu        sync.Mutex
	running   bool
}

// Start launches the extension binary via the JSON-RPC subprocess protocol.
func (ep *ExtensionProcess) Start() error {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if ep.running {
		return nil
	}

	ep.Cmd = exec.Command(ep.Manifest.Binary)
	ep.Cmd.Stdin = strings.NewReader("")
	ep.StartTime = time.Now()

	if err := ep.Cmd.Start(); err != nil {
		return fmt.Errorf("start extension %q: %w", ep.Manifest.Name, err)
	}
	ep.running = true
	return nil
}

// Stop terminates the extension process.
func (ep *ExtensionProcess) Stop() error {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if !ep.running || ep.Cmd == nil || ep.Cmd.Process == nil {
		return nil
	}
	if err := ep.Cmd.Process.Kill(); err != nil {
		return fmt.Errorf("stop extension %q: %w", ep.Manifest.Name, err)
	}
	ep.running = false
	return nil
}

// IsRunning returns true if the extension process is active.
func (ep *ExtensionProcess) IsRunning() bool {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.running
}

// ── Install / Uninstall ─────────────────────────────────────────────────

// InstallExtension copies an extension package to the extensions directory.
func (d *ExtensionDiscovery) InstallExtension(packagePath string) (*DiscoveredExtension, error) {
	m, err := ParseManifest(filepath.Join(packagePath, ExtensionManifestFile))
	if err != nil {
		return nil, fmt.Errorf("invalid extension package: %w", err)
	}

	destDir := ExtensionDir(d.baseDir, ExtensionCategory(m.Category), m.Name)
	if _, statErr := os.Stat(destDir); statErr == nil {
		return nil, fmt.Errorf("extension %q already installed at %s", m.Name, destDir)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create extension dir: %w", err)
	}

	if err := copyFile(filepath.Join(packagePath, ExtensionManifestFile),
		filepath.Join(destDir, ExtensionManifestFile)); err != nil {
		os.RemoveAll(destDir)
		return nil, fmt.Errorf("copy manifest: %w", err)
	}

	if m.Binary != "" {
		if err := copyFile(filepath.Join(packagePath, m.Binary),
			filepath.Join(destDir, m.Binary)); err != nil {
			os.RemoveAll(destDir)
			return nil, fmt.Errorf("copy binary: %w", err)
		}
		os.Chmod(filepath.Join(destDir, m.Binary), 0755)
	}

	ext := &DiscoveredExtension{
		Manifest:    *m,
		InstallPath: destDir,
		Enabled:     true,
		Health:      "unknown",
	}
	if m.Binary != "" {
		ext.BinaryPath = filepath.Join(destDir, m.Binary)
	}
	return ext, nil
}

// UninstallExtension removes an extension by category and name.
func (d *ExtensionDiscovery) UninstallExtension(category, name string) error {
	extDir := ExtensionDir(d.baseDir, ExtensionCategory(category), name)
	if _, err := os.Stat(extDir); os.IsNotExist(err) {
		return fmt.Errorf("extension %q not found", name)
	}
	return os.RemoveAll(extDir)
}

// ── Extension Tool Interface ─────────────────────────────────────

// ExtensionTool is implemented by extensions that provide tools.
type ExtensionTool interface {
	Name() string
	Version() string
	Tools() []Tool
}

// ── Helpers ─────────────────────────────────────────────────────────────

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
