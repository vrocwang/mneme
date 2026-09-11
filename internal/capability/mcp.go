package capability

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// NewMCPServerSet builds the capability-set descriptor for an MCP server. Every
// install, reconnect, and discovery path constructs server sets through it, so
// the ID scheme, kind, health, and config encoding are defined in one place
// instead of being re-derived from an identical literal at each call site.
func NewMCPServerSet(entry ServerEntry) *CapabilitySet {
	cfg, err := json.Marshal(entry)
	if err != nil {
		slog.Warn("failed to marshal MCP server entry", "name", entry.Name, "error", err)
		cfg = nil
	}
	desc := "External MCP server"
	if entry.Transport != "" {
		desc = fmt.Sprintf("MCP server: %s (%s)", entry.Name, entry.Transport)
	}
	return &CapabilitySet{
		ID:          MCPSetID(entry.Name),
		Name:        entry.Name,
		Kind:        KindMCPServer,
		Description: desc,
		Config:      cfg,
		Enabled:     entry.Enabled,
		Health:      HealthUnknown,
	}
}

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
	setID := MCPSetID(srv.Name)

	set := NewMCPServerSet(srv)
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
