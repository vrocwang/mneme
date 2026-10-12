package tools

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/simon/mneme/internal/workspace"
)

// ── Layout constants ───────────────────────────────────────────────────
//
// Aliases of the workspace layout SSOT so the on-disk names are defined once.
const (
	ExtensionsDirName     = workspace.DirExtensions
	ExtensionManifestFile = workspace.FileExtensionManifest
)

// ── Category ────────────────────────────────────────────────────────────

// ExtensionCategory groups extensions by function. The concrete category
// values come from each manifest, so no enum of them is declared here.
type ExtensionCategory string

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
	Build string `json:"build,omitempty"`
	// Eager forces this extension to start during discovery even when
	// `[extensions] lazy` is enabled. Declare it for an extension that does
	// work outside of tool calls — a listener, a poller, a service it must keep
	// running — because a lazily declared extension has no process until one of
	// its tools is called.
	Eager    bool   `json:"eager,omitempty"`
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
}

// ExtensionDiscovery scans the extensions directory and discovers installed
// extensions by reading their manifest.json files. The capability package
// uses ExtensionManifest directly (via this package) for startup discovery.
// ExtensionDiscovery provides the runtime management API for install and
// uninstall operations exposed via the frontend.
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

// FindExtensionDirs returns, in lexical order, every directory under root that
// holds a manifest.json. The walk does not descend into an extension's own
// subtree and skips hidden directories.
//
// It is the single traversal shared by startup discovery
// (capability.Bootstrap) and the UI-facing extension list
// (ExtensionDiscovery.Discover). Installed extensions live at
// <root>/<category>/<name> while bundled ones live at <root>/<name>, so a flat
// scan would make a UI-installed extension visible in the list but invisible to
// the loader (or vice versa) — the two paths must agree on depth.
func FindExtensionDirs(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable branch: skip it, never abort the whole scan
		}
		if !d.IsDir() {
			return nil
		}
		if path == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(path, ExtensionManifestFile)); statErr == nil {
			out = append(out, path)
			return fs.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// Discover scans and returns all discovered extensions sorted by name.
func (d *ExtensionDiscovery) Discover() ([]DiscoveredExtension, error) {
	extDir := d.ExtensionsDir()
	if _, err := os.Stat(extDir); os.IsNotExist(err) {
		return nil, nil
	}

	var result []DiscoveredExtension
	for _, dir := range FindExtensionDirs(extDir) {
		m, parseErr := ParseManifest(filepath.Join(dir, ExtensionManifestFile))
		if parseErr != nil {
			continue
		}

		ext := DiscoveredExtension{
			Manifest:    *m,
			InstallPath: dir,
			Enabled:     true,
			Health:      "unknown",
		}
		if m.Binary != "" {
			ext.BinaryPath = filepath.Join(dir, m.Binary)
		}
		result = append(result, ext)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Manifest.Name < result[j].Manifest.Name
	})
	return result, nil
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

// ExtensionDir returns the expected installation path for an extension.
func ExtensionDir(workspaceDir string, category ExtensionCategory, name string) string {
	return filepath.Join(workspaceDir, ExtensionsDirName, string(category), name)
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

// ── Helpers ─────────────────────────────────────────────────────────────

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
