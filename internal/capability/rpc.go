package capability

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/internal/tools"
	ws "github.com/simon/mneme/internal/workspace"
	"gopkg.in/yaml.v3"
)

// CapabilityRPC provides Wails-bound methods for the frontend capability page.
// The registry is set during app startup (NewCapabilityRPC/SetRegistry).
//
// Readiness policy — one rule for every exported method:
//
//   - read-only queries (ListSets, GetSet, ListAllTools, GetToolSchema,
//     ListAllAgents, GetToolDiagnostics, ListExtensions) return an empty
//     result (nil slice/map, zero value) when the registry is not ready. They
//     never error and never panic.
//   - mutating operations (AddMCPServer, RemoveMCPServer, ConnectMCPServer,
//     DisconnectMCPServer, InstallExtension, UninstallExtension, InstallSkill)
//     return an explicit "registry not ready" error when the registry is not
//     ready, so a mutation that did nothing is never reported as success.
//
// Previously the mutating set was split: the extension and agent-CRUD paths
// errored, while the MCP mutations and InstallSkill returned nil — a silently
// dropped mutation. Both halves now follow the rule above.
type CapabilityRPC struct {
	reg          *CapabilityRegistry
	mcpPersister MCPServerPersister
	workspace    string
}

// errRegistryNotReady is the error every mutating method returns while the
// registry is unset. It is a single value so callers (and tests) can compare
// with errors.Is instead of matching the message.
var errRegistryNotReady = errors.New("registry not ready")

// MCPServerPersister persists MCP server installations across restarts.
// Implemented by mcp/store.Store (which imports capability), so the cycle is
// broken by defining the interface and DTO here rather than importing the store.
type MCPServerPersister interface {
	PersistMCPServer(srv *PersistedMCPServer) error
	RemoveMCPServer(serverID string) error
}

// PersistedMCPServer is the capability-package view of an installed MCP server,
// decoupled from mcp/store.InstalledServer to avoid an import cycle.
type PersistedMCPServer struct {
	ServerID      string
	QualifiedName string
	DisplayName   string
	Command       string
	Args          []string
	Transport     string
	DeploymentURL string
	Enabled       bool
}

// NewCapabilityRPC creates a Wails RPC handler. reg may be nil until startup.
func NewCapabilityRPC(reg *CapabilityRegistry) *CapabilityRPC {
	return &CapabilityRPC{reg: reg}
}

// SetMCPPersister wires the persistence backend used to survive restarts.
// When nil (e.g. no database), Add/Remove operate in-memory only.
func (r *CapabilityRPC) SetMCPPersister(p MCPServerPersister) { r.mcpPersister = p }

// SetCapabilityWorkspace sets the active workspace root. Struct-qualified
// because Wails binds every exported method and config.ConfigRPC.SetWorkspace
// is the one the frontend calls. Every install/scan path the RPC
// touches (extensions, skills, user agents) is derived from it, so no path is
// reconstructed from a sibling directory and the layout stays owned by
// internal/workspace.
func (r *CapabilityRPC) SetCapabilityWorkspace(dir string) { r.workspace = dir }

// SetRegistry updates the registry reference. Called after startup creates it.
func (r *CapabilityRPC) SetRegistry(reg *CapabilityRegistry) { r.reg = reg }

func (r *CapabilityRPC) ok() bool { return r.reg != nil }

// skillsDir returns workspace/skills or an error when no workspace is set.
func (r *CapabilityRPC) skillsDir() (string, error) {
	if r.workspace == "" {
		return "", fmt.Errorf("workspace not configured")
	}
	return ws.SkillsDir(r.workspace), nil
}

// ── Set queries ───────────────────────────────────────

func (r *CapabilityRPC) ListSets() []*CapabilitySet {
	if !r.ok() {
		return nil
	}
	return r.reg.ListSets()
}

func (r *CapabilityRPC) GetSet(id string) (*CapabilitySet, bool) {
	if !r.ok() {
		return nil, false
	}
	return r.reg.GetSet(id)
}

// ── Tool queries ──────────────────────────────────────

func (r *CapabilityRPC) ListAllTools() []ToolDescriptor {
	if !r.ok() {
		return nil
	}
	return r.reg.AllTools()
}

func (r *CapabilityRPC) GetToolSchema(name string) json.RawMessage {
	if !r.ok() {
		return nil
	}
	t, ok := r.reg.GetTool(name)
	if !ok {
		return nil
	}
	return schemaToJSON(t.Schema().Parameters)
}

// ── Agent queries ─────────────────────────────────────

func (r *CapabilityRPC) ListAllAgents() []AgentDescriptor {
	if !r.ok() {
		return nil
	}
	return r.reg.AllAgents()
}

// GetToolDiagnostics returns diagnostic information about registered tools.
//
// MCP sets are split by the transport recorded in their config: mcpStdioTools
// counts stdio sets only, mcpHTTPTools counts every other MCP transport. Before
// this split both kinds were reported as "stdio" because every MCP set was
// counted into mcpStdioTools.
func (r *CapabilityRPC) GetToolDiagnostics() map[string]interface{} {
	if !r.ok() {
		return map[string]interface{}{
			"ok": false, "totalTools": 0, "enabledTools": 0,
			"mcpStdioTools": 0, "mcpHTTPTools": 0,
		}
	}

	allTools := r.reg.AllTools()
	sets := r.reg.ListSets()

	totalTools := len(allTools)
	enabledTools := 0
	mcpStdioTools := 0
	mcpHTTPTools := 0
	jsonRpcTools := 0
	inProcessTools := 0
	writeSurfaces := 0
	bySource := make(map[string]int)

	for _, set := range sets {
		count := set.ToolCount
		bySource[set.ID] = count
		if set.Enabled {
			enabledTools += count
		}
		switch set.Kind {
		case KindMCPServer:
			if mcpSetTransport(set) == "stdio" {
				mcpStdioTools += count
			} else {
				mcpHTTPTools += count
			}
		case KindExtension:
			jsonRpcTools += count
		case KindBuiltin:
			inProcessTools += count
		}
	}

	// Estimate write surfaces (tools with side effects).
	for _, t := range allTools {
		if t.HasSideEffects {
			writeSurfaces++
		}
	}

	return map[string]interface{}{
		"ok":             true,
		"totalTools":     totalTools,
		"enabledTools":   enabledTools,
		"mcpStdioTools":  mcpStdioTools,
		"mcpHTTPTools":   mcpHTTPTools,
		"jsonRpcTools":   jsonRpcTools,
		"inProcessTools": inProcessTools,
		"writeSurfaces":  writeSurfaces,
		"bySource":       bySource,
	}
}

// mcpSetTransport reports the transport recorded in an MCP set's Config
// (ServerEntry). An empty or unparseable config reports "stdio": every MCP set
// built by NewMCPServerSet carries a transport, and stdio is the historical
// default for sets created before the field existed.
func mcpSetTransport(set *CapabilitySet) string {
	if set == nil || len(set.Config) == 0 {
		return "stdio"
	}
	var entry ServerEntry
	if err := json.Unmarshal(set.Config, &entry); err != nil || entry.Transport == "" {
		return "stdio"
	}
	return entry.Transport
}

// ── MCP management ────────────────────────────────────

func (r *CapabilityRPC) AddMCPServer(name, transport, command, url string, args []string) error {
	if !r.ok() {
		return errRegistryNotReady
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}
	// Reject a duplicate before touching the registry: AddSet would fail
	// anyway, but an explicit message tells the caller how to recover.
	if _, exists := r.reg.GetSet(MCPSetID(name)); exists {
		return fmt.Errorf("server %q is already installed", name)
	}
	if transport != "stdio" && transport != "http" {
		return fmt.Errorf("unknown transport %q: use 'stdio' or 'http'", transport)
	}
	if transport == "stdio" && command == "" {
		return fmt.Errorf("command is required for stdio transport")
	}
	if transport == "http" && url == "" {
		return fmt.Errorf("url is required for http transport")
	}
	entryObj := ServerEntry{
		Name: name, Transport: transport, Command: command, Args: args, URL: url, Enabled: true,
	}
	set := NewMCPServerSet(entryObj)
	if err := r.reg.AddSet(set); err != nil {
		return err
	}
	if err := r.reg.ConnectMCPServer(MCPSetID(name), entryObj); err != nil {
		// Rollback the just-added set so a retry with corrected config is not
		// blocked by a stale "already registered" entry.
		_ = r.reg.RemoveSet(MCPSetID(name))
		return fmt.Errorf("connect failed: %w", err)
	}
	// Persist so ReconnectPersistedServers re-connects it after restart.
	if r.mcpPersister != nil {
		if perr := r.mcpPersister.PersistMCPServer(&PersistedMCPServer{
			ServerID: MCPSetID(name), QualifiedName: name, DisplayName: name,
			Command: command, Args: args, Transport: transport,
			DeploymentURL: url, Enabled: true,
		}); perr != nil {
			// Non-fatal: the server works for this session but won't reconnect
			// after a restart, so make the reason visible.
			slog.Warn("MCP server persist failed; it will not reconnect after restart",
				"server", name, "error", perr)
		}
	}
	return nil
}

func (r *CapabilityRPC) RemoveMCPServer(name string) error {
	if !r.ok() {
		return errRegistryNotReady
	}
	r.reg.DisconnectMCPServer(MCPSetID(name))
	err := r.reg.RemoveSet(MCPSetID(name))
	if r.mcpPersister != nil {
		// Always clean up persisted state, even if the in-memory set was
		// already absent (e.g. after a restart that failed to reconnect).
		if perr := r.mcpPersister.RemoveMCPServer(MCPSetID(name)); perr != nil {
			return perr
		}
		// Suppress a "set not found" error: removing a server that exists
		// only in the DB is not a failure.
		return nil
	}
	return err
}

func (r *CapabilityRPC) ConnectMCPServer(name string) error {
	if !r.ok() {
		return errRegistryNotReady
	}
	set, ok := r.reg.GetSet(MCPSetID(name))
	if !ok {
		return nil
	}
	var entry ServerEntry
	if err := json.Unmarshal(set.Config, &entry); err != nil {
		return fmt.Errorf("set %q: invalid MCP config: %w", name, err)
	}
	return r.reg.ConnectMCPServer(MCPSetID(name), entry)
}

func (r *CapabilityRPC) DisconnectMCPServer(name string) error {
	if !r.ok() {
		return errRegistryNotReady
	}
	return r.reg.DisconnectMCPServer(MCPSetID(name))
}

// ── Extension management ────────────────────────────────

// ExtensionInfo is the RPC-facing type for extensions.
type ExtensionInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Category    string `json:"category"`
	Description string `json:"description,omitempty"`
	InstallPath string `json:"installPath"`
	Enabled     bool   `json:"enabled"`
	Loaded      bool   `json:"loaded"`
	Health      string `json:"health"`
	// Started is true while the extension's process is running. Lazy marks an
	// extension declared from the handshake cache: it has a complete tool
	// surface but no process until a tool is used, and its process is stopped
	// again when idle. A lazy, not-started extension is healthy, not down.
	Started  bool   `json:"started"`
	Lazy     bool   `json:"lazy,omitempty"`
	Author   string `json:"author,omitempty"`
	Homepage string `json:"homepage,omitempty"`
}

// ListExtensions returns all extensions — both loaded (from registry) and
// installed on disk but not yet loaded (added after startup).
func (r *CapabilityRPC) ListExtensions() []ExtensionInfo {
	if !r.ok() {
		return nil
	}
	// Start with loaded extensions from the registry.
	loaded := map[string]bool{}
	var result []ExtensionInfo
	for _, set := range r.reg.ListSetsByKind(KindExtension) {
		loaded[set.Name] = true
		result = append(result, ExtensionInfo{
			Name:        set.Name,
			Description: set.Description,
			Enabled:     set.Enabled,
			Loaded:      true,
			Health:      string(set.Health),
			Started:     set.Started,
			Lazy:        set.Lazy,
		})
	}

	// Scan disk for extensions not yet loaded.
	for _, dir := range DefaultExtensionDirs(r.workspace) {
		discovery := tools.NewExtensionDiscovery(dir)
		discovered, _ := discovery.Discover()
		for _, d := range discovered {
			if loaded[d.Manifest.Name] {
				continue
			}
			loaded[d.Manifest.Name] = true
			result = append(result, ExtensionInfo{
				Name:        d.Manifest.Name,
				Version:     d.Manifest.Version,
				Category:    string(d.Manifest.Category),
				Description: d.Manifest.Description,
				InstallPath: d.InstallPath,
				Enabled:     d.Enabled,
				Loaded:      false,
				Health:      "unknown",
				Author:      d.Manifest.Author,
				Homepage:    d.Manifest.Homepage,
			})
		}
	}
	return result
}

// InstallExtension installs an extension from a directory path.
func (r *CapabilityRPC) InstallExtension(packagePath string) error {
	if !r.ok() {
		return errRegistryNotReady
	}
	if r.workspace == "" {
		return fmt.Errorf("workspace not configured")
	}
	discovery := tools.NewExtensionDiscovery(r.workspace)
	ext, err := discovery.InstallExtension(packagePath)
	if err != nil {
		return err
	}
	// Load immediately: build, start, register tools/agents.
	return loadExtension(r.reg, ext, r.workspace, nil)
}

// UninstallExtension removes an extension by category and name.
func (r *CapabilityRPC) UninstallExtension(category, name string) error {
	if !r.ok() {
		return errRegistryNotReady
	}
	if r.workspace == "" {
		return fmt.Errorf("workspace not configured")
	}
	discovery := tools.NewExtensionDiscovery(r.workspace)
	if err := discovery.UninstallExtension(category, name); err != nil {
		return err
	}
	// Remove from registry so it disappears immediately.
	setID := ExtensionSetID(name)
	r.reg.RemoveSet(setID)
	return nil
}

// ── Skill install ──────────────────────────────────────────

// InstallSkill installs a skill from a local SKILL.md file or hub URL.
// Local: path to a SKILL.md file with YAML frontmatter.
// URL: http/https URL to a SKILL.md file (e.g. raw GitHub URL).
// The skill is written to skills/<name>/SKILL.md and registered as KindSkill.
func (r *CapabilityRPC) InstallSkill(source string) error {
	if !r.ok() {
		return errRegistryNotReady
	}
	skillsDir, dirErr := r.skillsDir()
	if dirErr != nil {
		return dirErr
	}
	var data []byte
	var err error
	if isURL(source) {
		data, err = downloadSkillURL(source)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return fmt.Errorf("read skill: %w", err)
	}
	m, _, err := parseSkillFrontmatter(data)
	if err != nil {
		return fmt.Errorf("parse SKILL.md: %w", err)
	}
	if m.Name == "" {
		return fmt.Errorf("SKILL.md missing required 'name' field in YAML frontmatter")
	}
	// Write to skills/<name>/SKILL.md
	skillDir := filepath.Join(skillsDir, m.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	// Preserve original source for URL-installed skills
	if err := os.WriteFile(filepath.Join(skillDir, ws.FileSkill), data, 0644); err != nil {
		return err
	}
	return r.registerSkill(*m)
}

func downloadSkillURL(rawURL string) ([]byte, error) {
	// Rewrite GitHub blob URLs to raw
	rawURL = strings.Replace(rawURL, "github.com/", "raw.githubusercontent.com/", 1)
	rawURL = strings.Replace(rawURL, "/blob/", "/", 1)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mneme-go/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch %s: HTTP %d", rawURL, resp.StatusCode)
	}
	// Cap at 1 MiB
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	// Reject HTML pages - the URL points to a catalog page (e.g. skills.sh),
	// not a raw SKILL.md. Prompt for a raw URL instead of writing garbage.
	if isHTMLResponse(resp.Header.Get("Content-Type"), data) {
		return nil, fmt.Errorf("URL %s returned an HTML page, not a SKILL.md file; provide a raw URL (e.g. https://raw.githubusercontent.com/<owner>/<repo>/main/<path>/SKILL.md)", rawURL)
	}
	return data, nil
}

// isHTMLResponse reports whether the response is an HTML page rather than a
// raw SKILL.md document, by content-type or leading content prefix.
func isHTMLResponse(contentType string, data []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return true
	}
	trimmed := strings.TrimLeft(string(data), " \t\r\n")
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html")
}

// parseSkillFrontmatter extracts YAML frontmatter from a SKILL.md file.
// Frontmatter is delimited by --- lines. Returns the parsed manifest and body.
func parseSkillFrontmatter(data []byte) (*SkillManifest, string, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return nil, "", fmt.Errorf("SKILL.md must start with YAML frontmatter (---)")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return nil, "", fmt.Errorf("SKILL.md: missing closing --- for YAML frontmatter")
	}
	yamlBlock := text[4 : 4+end]
	body := text[4+end+5:]

	var m SkillManifest
	if err := yaml.Unmarshal([]byte(yamlBlock), &m); err != nil {
		return nil, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	return &m, body, nil
}

// registerSkill registers a parsed skill manifest as a capability set. The
// SKILL.md itself is already on disk; only the registry entry is created here.
func (r *CapabilityRPC) registerSkill(m SkillManifest) error {
	return registerSkillSet(r.reg, m)
}

func isURL(s string) bool {
	return len(s) > 8 && (s[:7] == "http://" || s[:8] == "https://")
}

// loadExtension builds, starts, and registers a newly installed extension
// so it is immediately available without a restart.
func loadExtension(reg *CapabilityRegistry, ext *tools.DiscoveredExtension, workspace string, log *slog.Logger) error {
	if ext == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}

	// Resolve the binary path: may need building, or may be a pre-built binary / script.
	discovery := tools.NewExtensionDiscovery(workspace)
	binaryPath, err := discovery.AutoBuild(ext)
	if err != nil {
		// If AutoBuild fails (e.g. no build command and binary missing),
		// try the BinaryPath from manifest resolution directly.
		if ext.BinaryPath == "" {
			return fmt.Errorf("extension %q has no binary and build failed: %w", ext.Manifest.Name, err)
		}
		binaryPath = ext.BinaryPath
	}
	log.Info("loading installed extension", "name", ext.Manifest.Name, "binary", binaryPath)

	// Load and register through the single shared loading path.
	// RegisterExtension fails closed: on a duplicate set ID it returns before
	// mutating any state, and it tracks the process for Shutdown/dispose.
	_, err = launchExtensionProc(reg, binaryPath, &ext.Manifest, nil, log)
	return err
}
