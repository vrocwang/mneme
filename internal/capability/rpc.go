package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	agenttoml "github.com/simon/mneme/internal/agent/toml"
	"github.com/simon/mneme/internal/tools"
	"gopkg.in/yaml.v3"
)

// CapabilityRPC provides Wails-bound methods for the frontend capability page.
// The registry is set during app startup; methods return empty results before that.
type CapabilityRPC struct {
	reg                 *CapabilityRegistry
	mcpPersister        MCPServerPersister
	legacyExtensionsDir string
	skillsDir           string
}

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

// SetLegacyExtensionsDir sets the extensions directory path for InstallLegacyExtension.
func (r *CapabilityRPC) SetLegacyExtensionsDir(dir string) { r.legacyExtensionsDir = dir }

// SetSkillsDir sets the skills directory path for InstallSkill.
func (r *CapabilityRPC) SetSkillsDir(dir string) { r.skillsDir = dir }

// SetRegistry updates the registry reference. Called after startup creates it.
func (r *CapabilityRPC) SetRegistry(reg *CapabilityRegistry) { r.reg = reg }

func (r *CapabilityRPC) ok() bool { return r.reg != nil }

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
func (r *CapabilityRPC) GetToolDiagnostics() map[string]interface{} {
	if !r.ok() {
		return map[string]interface{}{"ok": false, "totalTools": 0, "enabledTools": 0}
	}

	allTools := r.reg.AllTools()
	sets := r.reg.ListSets()

	totalTools := len(allTools)
	enabledTools := 0
	mcpStdioTools := 0
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
			mcpStdioTools += count
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
		"jsonRpcTools":   jsonRpcTools,
		"inProcessTools": inProcessTools,
		"writeSurfaces":  writeSurfaces,
		"bySource":       bySource,
		"recentDenials":  []map[string]interface{}{},
	}
}

// ── MCP management ────────────────────────────────────

// ── Agent CRUD ───────────────────────────────────────

func (r *CapabilityRPC) CreateAgent(def map[string]interface{}) error {
	if !r.ok() {
		return fmt.Errorf("registry not ready")
	}
	agentDef := &tools.AgentDef{}
	b, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal agent def: %w", err)
	}
	if err := json.Unmarshal(b, agentDef); err != nil {
		return fmt.Errorf("invalid agent definition: %w", err)
	}
	if agentDef.ID == "" || agentDef.Name == "" {
		return fmt.Errorf("agent id and name are required")
	}
	agentsDir := filepath.Join(r.legacyExtensionsDir, "..", "agents")
	if err := agenttoml.SaveAgentToFile(agentsDir, agentDef); err != nil {
		return fmt.Errorf("save agent: %w", err)
	}
	r.reg.RegisterAgent("builtin", agentDef)
	return nil
}

func (r *CapabilityRPC) UpdateAgent(id string, def map[string]interface{}) error {
	if !r.ok() {
		return fmt.Errorf("registry not ready")
	}
	existing, ok := r.reg.GetAgent(id)
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	agentDef := existing
	b, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal agent def: %w", err)
	}
	if err := json.Unmarshal(b, agentDef); err != nil {
		return fmt.Errorf("invalid agent definition: %w", err)
	}
	agentDef.ID = id
	agentsDir := filepath.Join(r.legacyExtensionsDir, "..", "agents")
	if err := agenttoml.SaveAgentToFile(agentsDir, agentDef); err != nil {
		return fmt.Errorf("save agent: %w", err)
	}
	r.reg.RegisterAgent("builtin", agentDef)
	return nil
}

func (r *CapabilityRPC) DeleteAgent(id string) error {
	if !r.ok() {
		return fmt.Errorf("registry not ready")
	}
	if _, ok := r.reg.GetAgent(id); !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	agentsDir := filepath.Join(r.legacyExtensionsDir, "..", "agents")
	if err := agenttoml.DeleteAgentFile(agentsDir, id); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	r.reg.UnregisterAgent(id)
	return nil
}

func (r *CapabilityRPC) AddMCPServer(name, transport, command, url string, args []string) error {
	if !r.ok() {
		return nil
	}
	entryObj := ServerEntry{
		Name: name, Transport: transport, Command: command, Args: args, URL: url, Enabled: true,
	}
	entry, err := json.Marshal(entryObj)
	if err != nil {
		return fmt.Errorf("marshal server entry: %w", err)
	}
	set := &CapabilitySet{
		ID: "mcp:" + name, Name: name, Kind: KindMCPServer,
		Description: "External MCP server", Config: entry, Enabled: true, Health: HealthUnknown,
	}
	if err := r.reg.AddSet(set); err != nil {
		return err
	}
	if err := r.reg.ConnectMCPServer("mcp:"+name, entryObj); err != nil {
		// Rollback the just-added set so a retry with corrected config is not
		// blocked by a stale "already registered" entry.
		_ = r.reg.RemoveSet("mcp:" + name)
		return fmt.Errorf("connect failed: %w", err)
	}
	// Persist so ReconnectPersistedServers re-connects it after restart.
	if r.mcpPersister != nil {
		if perr := r.mcpPersister.PersistMCPServer(&PersistedMCPServer{
			ServerID: "mcp:" + name, QualifiedName: name, DisplayName: name,
			Command: command, Args: args, Transport: transport,
			DeploymentURL: url, Enabled: true,
		}); perr != nil {
			// Non-fatal: the server works for this session but won't reconnect.
		}
	}
	return nil
}

func (r *CapabilityRPC) RemoveMCPServer(name string) error {
	if !r.ok() {
		return nil
	}
	r.reg.DisconnectMCPServer("mcp:" + name)
	err := r.reg.RemoveSet("mcp:" + name)
	if r.mcpPersister != nil {
		// Always clean up persisted state, even if the in-memory set was
		// already absent (e.g. after a restart that failed to reconnect).
		if perr := r.mcpPersister.RemoveMCPServer("mcp:" + name); perr != nil {
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
		return nil
	}
	set, ok := r.reg.GetSet("mcp:" + name)
	if !ok {
		return nil
	}
	var entry ServerEntry
	json.Unmarshal(set.Config, &entry)
	return r.reg.ConnectMCPServer("mcp:"+name, entry)
}

func (r *CapabilityRPC) DisconnectMCPServer(name string) error {
	if !r.ok() {
		return nil
	}
	return r.reg.DisconnectMCPServer("mcp:" + name)
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
	Author      string `json:"author,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
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
		})
	}

	// Scan disk for extensions not yet loaded.
	workspace := filepath.Dir(r.legacyExtensionsDir)
	if workspace == "" || workspace == "." {
		workspace, _ = os.Getwd()
	}
	for _, dir := range DefaultExtensionDirs(workspace) {
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
	workspace := filepath.Dir(r.legacyExtensionsDir)
	if workspace == "" || workspace == "." {
		return fmt.Errorf("workspace not configured")
	}
	discovery := tools.NewExtensionDiscovery(workspace)
	ext, err := discovery.InstallExtension(packagePath)
	if err != nil {
		return err
	}
	// Load immediately: build, start, register tools/agents.
	return loadExtension(r.reg, ext, workspace, nil)
}

// UninstallExtension removes an extension by category and name.
func (r *CapabilityRPC) UninstallExtension(category, name string) error {
	workspace := filepath.Dir(r.legacyExtensionsDir)
	discovery := tools.NewExtensionDiscovery(workspace)
	if err := discovery.UninstallExtension(category, name); err != nil {
		return err
	}
	// Remove from registry so it disappears immediately.
	setID := "extension:" + name
	r.reg.RemoveSet(setID)
	return nil
}

// ── Extension / Skill install ──────────────────────────────

// InstallLegacyExtension copies an extension file into the extensions directory, makes it
// executable, and reloads extension discovery. Supports any executable:
// Go binaries, shell scripts, Python, Ruby, Node.js (via shebang).
func (r *CapabilityRPC) InstallLegacyExtension(sourcePath string) error {
	if !r.ok() || r.legacyExtensionsDir == "" {
		return nil
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return err
	}
	if err := os.MkdirAll(r.legacyExtensionsDir, 0755); err != nil {
		return err
	}
	base := filepath.Base(sourcePath)
	dest := filepath.Join(r.legacyExtensionsDir, base)
	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	if err := os.Chmod(dest, 0755); err != nil {
		return err
	}
	_, err = discoverLegacyExtensions(r.reg, r.legacyExtensionsDir, slog.Default())
	return err
}

// InstallSkill installs a skill from a local SKILL.md file or hub URL.
// Local: path to a SKILL.md file with YAML frontmatter.
// URL: http/https URL to a SKILL.md file (e.g. raw GitHub URL).
// The skill is written to skills/<name>/SKILL.md and registered as KindSkill.
func (r *CapabilityRPC) InstallSkill(source string) error {
	if !r.ok() || r.skillsDir == "" {
		return nil
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
	m, body, err := parseSkillFrontmatter(data)
	if err != nil {
		return fmt.Errorf("parse SKILL.md: %w", err)
	}
	if m.Name == "" {
		return fmt.Errorf("SKILL.md missing required 'name' field in YAML frontmatter")
	}
	// Write to skills/<name>/SKILL.md
	skillDir := filepath.Join(r.skillsDir, m.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	// Preserve original source for URL-installed skills
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), data, 0644); err != nil {
		return err
	}
	return r.registerSkill(skillDir, *m, body)
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

func (r *CapabilityRPC) registerSkill(skillDir string, m SkillManifest, body string) error {
	_ = skillDir // kept for API compatibility
	_ = body
	return registerSkillSet(r.reg, m)
}

func isURL(s string) bool {
	return len(s) > 8 && (s[:7] == "http://" || s[:8] == "https://")
}

// ── Skill catalog ──────────────────────────────────────

// ListSkillCatalog fetches the aggregated skill registry catalog.
func (r *CapabilityRPC) ListSkillCatalog() ([]SkillCatalogEntry, error) {
	if !r.ok() {
		return nil, nil
	}
	return FetchSkillCatalog()
}

// RefreshSkillCatalog forces a catalog cache refresh.
func (r *CapabilityRPC) RefreshSkillCatalog() ([]SkillCatalogEntry, error) {
	if !r.ok() {
		return nil, nil
	}
	return RefreshSkillCatalog()
}

// InstallSkillFromCatalog installs a skill by its catalog entry ID.
func (r *CapabilityRPC) InstallSkillFromCatalog(entryID string) error {
	entries, err := FetchSkillCatalog()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.ID == entryID && e.DownloadURL != "" {
			return r.InstallSkill(e.DownloadURL)
		}
	}
	return fmt.Errorf("catalog entry %q not found or has no download URL", entryID)
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

	// Start the extension process.
	proc, err := tools.StartProtoFromCommand(context.Background(), binaryPath, log)
	if err != nil {
		return fmt.Errorf("start extension %q: %w", ext.Manifest.Name, err)
	}

	setID := "extension:" + ext.Manifest.Name
	// Register tools.
	if tools, err := proc.ListTools(context.Background()); err == nil {
		for _, t := range tools {
			reg.RegisterTool(setID, t)
		}
	}
	// Register agents.
	if agents, err := proc.ListAgents(context.Background()); err == nil {
		for _, a := range agents {
			aCopy := a
			reg.RegisterAgent(setID, &aCopy)
		}
	}

	reg.TrackExtension(setID, proc)
	set := &CapabilitySet{
		ID:          setID,
		Name:        ext.Manifest.Name,
		Kind:        KindExtension,
		Description: ext.Manifest.Description,
		Health:      HealthOK,
		Enabled:     true,
	}
	return reg.AddSet(set)
}
