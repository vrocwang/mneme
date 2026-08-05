package capability

import (
	"encoding/json"
	"log/slog"
)

// MCPEventFunc is called when an MCP server connects or disconnects.
// The capability package owns no event types — callers wire their own publishing.
type MCPEventFunc func(kind, serverName string, toolCount int)

// mcpEventFn is a package-level hook for MCP lifecycle events. Set via
// SetMCPEventHook before bootstrapping MCP servers.
var mcpEventFn MCPEventFunc

// SetMCPEventHook registers a callback for MCP connect/disconnect events.
func SetMCPEventHook(fn MCPEventFunc) { mcpEventFn = fn }

// connectMCPServer connects to an MCP server and registers its tools.
func connectMCPServer(reg *CapabilityRegistry, srv ServerEntry, log *slog.Logger) error {
	setID := "mcp:" + srv.Name

	entry, err := json.Marshal(srv)
	if err != nil {
		log.Warn("failed to marshal MCP server entry", "name", srv.Name, "error", err)
		entry = nil
	}

	set := &CapabilitySet{
		ID: setID, Name: srv.Name, Kind: KindMCPServer,
		Description: "External MCP server", Config: entry,
		Enabled: srv.Enabled, Health: HealthUnknown,
	}
	if err := reg.AddSet(set); err != nil {
		return err
	}
	if !srv.Enabled {
		return nil
	}
	if err := reg.ConnectMCPServer(setID, srv); err != nil {
		reg.UpdateSetHealth(setID, HealthDown)
		log.Warn("MCP server connect failed", "name", srv.Name, "error", err)
		if mcpEventFn != nil {
			mcpEventFn("disconnected", srv.Name, 0)
		}
		return err
	}
	log.Info("MCP server connected", "name", srv.Name, "tools", set.ToolCount)
	if mcpEventFn != nil {
		mcpEventFn("connected", srv.Name, set.ToolCount)
	}
	return nil
}
