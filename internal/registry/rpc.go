// Package registry provides the Wails-bindable RPC struct for the MCP
// management UI. The frontend invokes methods on go.registry.RPC via the
// generated wailsjs bindings (wailsjs/go/registry/RPC.js).
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/simon/mneme/internal/capability"
	mcpregistry "github.com/simon/mneme/internal/mcp/registry"
	mcpstore "github.com/simon/mneme/internal/mcp/store"
)

// MCPServerInfo is the lightweight server descriptor returned by ListMCPServers.
// Must match the generated TypeScript type in wailsjs/go/models.ts.
type MCPServerInfo struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Command   string `json:"command,omitempty"`
	URL       string `json:"url,omitempty"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
}

// MCPServerToolInfo describes a single tool from a connected MCP server.
type MCPServerToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MCPServerStatus is the detailed status for a single MCP server.
type MCPServerStatus struct {
	Name      string              `json:"name"`
	Connected bool                `json:"connected"`
	Tools     []MCPServerToolInfo `json:"tools"`
}

// RPC is the Wails-bindable RPC handler for MCP management. The frontend
// invokes methods on go.registry.RPC.
type RPC struct {
	reg      *capability.CapabilityRegistry
	store    *mcpstore.Store
	registry *mcpregistry.Client
	log      *slog.Logger
}

// NewRPC creates an RPC handler. If store is non-nil, installed servers are
// persisted to SQLite; if registry is nil, one is created from the store for
// cache-backed Smithery queries.
func NewRPC(reg *capability.CapabilityRegistry, store *mcpstore.Store, log *slog.Logger) *RPC {
	if log == nil {
		log = slog.Default()
	}
	regClient := mcpregistry.NewClient(store)
	return &RPC{reg: reg, store: store, registry: regClient, log: log}
}

// SetRegistry allows tests to inject a mock registry client.
func (r *RPC) SetRegistry(rc *mcpregistry.Client) { r.registry = rc }

// Wire sets the live dependencies after construction. Called from app startup
// once the capability registry and MCP store are initialized.
func (r *RPC) Wire(reg *capability.CapabilityRegistry, store *mcpstore.Store) {
	r.reg = reg
	r.store = store
	if store != nil && r.registry == nil {
		r.registry = mcpregistry.NewClient(store)
	}
}

// ── ListMCPServers ──────────────────────────────────────────────────────

// ListMCPServers returns all installed MCP servers (from SQLite if available,
// falling back to the in-memory capability registry).
func (r *RPC) ListMCPServers() ([]MCPServerInfo, error) {
	if r.store != nil {
		servers, err := r.store.ListServers()
		if err == nil {
			out := make([]MCPServerInfo, 0, len(servers))
			for _, s := range servers {
				out = append(out, MCPServerInfo{
					Name:      s.QualifiedName,
					Transport: s.Transport,
					Command:   s.Command,
					URL:       s.DeploymentURL,
					Enabled:   s.Enabled,
				})
			}
			return out, nil
		}
		r.log.Warn("mcp rpc: list from store failed, falling back to memory", "error", err)
	}

	sets := r.reg.ListSetsByKind(capability.KindMCPServer)
	out := make([]MCPServerInfo, 0, len(sets))
	for _, s := range sets {
		var entry capability.ServerEntry
		json.Unmarshal(s.Config, &entry)
		out = append(out, MCPServerInfo{
			Name:      s.Name,
			Transport: entry.Transport,
			Command:   entry.Command,
			URL:       entry.URL,
			Enabled:   s.Enabled,
			Connected: s.Health == capability.HealthOK,
		})
	}
	return out, nil
}

// ── InstallMCPServer ─────────────────────────────────────────────────────

// InstallMCPServer installs an MCP server. The frontend passes either a
// registry name (in which case we resolve it) or manual transport details.
func (r *RPC) InstallMCPServer(name, transport, command, url string, args []string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	setID := "mcp:" + name
	if _, ok := r.reg.GetSet(setID); ok {
		return fmt.Errorf("server %q is already installed", name)
	}

	var entry capability.ServerEntry
	// Try registry resolution first when no explicit transport is given.
	if transport == "" && r.registry != nil {
		resolved, err := r.registry.ResolveInstall(context.Background(), name)
		if err == nil {
			entry = capability.ServerEntry{
				Name: name, Transport: resolved.Transport, Command: resolved.Command,
				Args: resolved.Args, URL: resolved.DeploymentURL, Enabled: true,
			}
		}
	}
	if entry.Transport == "" {
		if transport == "" {
			return fmt.Errorf("transport is required (or use a registry name for auto-resolution)")
		}
		if transport != "stdio" && transport != "http" {
			return fmt.Errorf("unknown transport %q", transport)
		}
		entry = capability.ServerEntry{
			Name: name, Transport: transport, Command: command, Args: args, URL: url, Enabled: true,
		}
	}

	entryJSON, _ := json.Marshal(entry)
	set := &capability.CapabilitySet{
		ID: setID, Name: name, Kind: capability.KindMCPServer,
		Description: fmt.Sprintf("MCP server: %s (%s)", name, entry.Transport),
		Config:      entryJSON, Enabled: true, Health: capability.HealthUnknown,
	}
	if err := r.reg.AddSet(set); err != nil {
		return err
	}
	if err := r.reg.ConnectMCPServer(setID, entry); err != nil {
		r.reg.UpdateSetHealth(setID, capability.HealthDown)
		return fmt.Errorf("connect failed: %w", err)
	}

	if r.store != nil {
		inst := &mcpstore.InstalledServer{
			ServerID:      setID,
			QualifiedName: name,
			DisplayName:   name,
			Command:       entry.Command,
			Args:          entry.Args,
			Transport:     entry.Transport,
			DeploymentURL: entry.URL,
			Enabled:       true,
		}
		if err := r.store.InsertServer(inst); err != nil {
			r.log.Warn("mcp rpc: persist install failed", "name", name, "error", err)
		}
	}
	return nil
}

// ── UninstallMCPServer ────────────────────────────────────────────────────

// UninstallMCPServer disconnects and removes an installed MCP server.
func (r *RPC) UninstallMCPServer(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	setID := "mcp:" + name
	_ = r.reg.DisconnectMCPServer(setID)
	if err := r.reg.RemoveSet(setID); err != nil {
		return err
	}
	if r.store != nil {
		if _, err := r.store.DeleteServer(setID); err != nil {
			r.log.Warn("mcp rpc: persist uninstall failed", "name", name, "error", err)
		}
	}
	return nil
}

// ── GetMCPServerStatus ────────────────────────────────────────────────────

// GetMCPServerStatus returns detailed connection status for a single server.
func (r *RPC) GetMCPServerStatus(name string) (MCPServerStatus, error) {
	if name == "" {
		return MCPServerStatus{}, fmt.Errorf("name is required")
	}
	setID := "mcp:" + name
	set, ok := r.reg.GetSet(setID)
	if !ok {
		return MCPServerStatus{Name: name, Connected: false, Tools: nil}, nil
	}

	connected := set.Health == capability.HealthOK
	var tools []MCPServerToolInfo

	// Resolve tools from registry.
	allTools := r.reg.AllTools()
	for _, td := range allTools {
		if prefix := "mcp:" + name; len(td.Name) > len(prefix) {
			if td.Name[:len(prefix)] == prefix {
				tools = append(tools, MCPServerToolInfo{Name: td.Name, Description: td.Description})
			}
		}
	}

	return MCPServerStatus{
		Name:      name,
		Connected: connected,
		Tools:     tools,
	}, nil
}

// ── CallMCPTool ───────────────────────────────────────────────────────────

// CallMCPTool invokes a tool on a connected MCP server.
func (r *RPC) CallMCPTool(serverName, toolName string, args map[string]interface{}) (map[string]interface{}, error) {
	if serverName == "" || toolName == "" {
		return nil, fmt.Errorf("serverName and toolName are required")
	}
	// Resolve the full tool name: the registry prefixes tools with "mcp:<server>:"
	fullName := "mcp:" + serverName + ":" + toolName
	// Fallback: try without the double-colon separator
	if _, ok := r.reg.GetTool(fullName); !ok {
		fullName = toolName
		if _, ok := r.reg.GetTool(fullName); !ok {
			return map[string]interface{}{"output": fmt.Sprintf("tool %q not found on server %q", toolName, serverName), "isError": true}, nil
		}
	}

	result := r.reg.Execute(context.Background(), fullName, args)
	out := map[string]interface{}{
		"output":  result.Output,
		"isError": result.Error != "",
	}
	if result.Error != "" {
		out["output"] = result.Error
	}
	return out, nil
}
