package capability

import (
	"context"
	"fmt"
	"time"

	mcpclient "github.com/simon/mneme/internal/mcp/client"
	"github.com/simon/mneme/internal/tools"
)

// mcpProxyTool adapts a tool exposed by a connected MCP server to the
// standard tools.Tool seam. Registering MCP tools through the same exec map
// as local tools means AllTools/ToolNames/GetTool/Execute have exactly one
// code path; execution proxies to the server's client with audit logging.
type mcpProxyTool struct {
	name     string
	serverID string // owning capability set ID
	reg      *CapabilityRegistry
}

func (t *mcpProxyTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        t.name,
		Description: "MCP remote tool",
	}
}

func (t *mcpProxyTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	client, serverName := t.reg.mcpClientFor(t.serverID)
	if client == nil {
		return tools.Result{Error: fmt.Sprintf("MCP server for tool %q is not connected", t.name)}
	}

	start := time.Now()
	result, err := client.CallTool(ctx, t.name, args)
	elapsed := time.Since(start)

	if l := t.reg.auditLogger(); l != nil {
		success := err == nil && !result.IsError
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else if result.IsError {
			errMsg = fmt.Sprintf("%v", result.Content)
		}
		_ = l.Log(ctx, serverName, t.name, args, success, errMsg, elapsed)
	}

	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	return resultToToolsResult(result)
}

// mcpClientFor returns the live MCP client for a set ID and the server's
// display name, or nil when the server is not connected.
func (r *CapabilityRegistry) mcpClientFor(setID string) (*mcpclient.Client, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	client := r.mcpClients[setID]
	name := setID
	if s, ok := r.sets[setID]; ok {
		name = s.Name
	}
	return client, name
}

// auditLogger returns the optional MCP audit logger.
func (r *CapabilityRegistry) auditLogger() MCPAuditLogger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mcpAudit
}
