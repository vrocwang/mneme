package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/simon/mneme/internal/tools"
)

// MCPTool wraps an MCP server tool as an Mneme tool.
type MCPTool struct {
	tools.BaseTool
	serverName string
	capReg     *CapabilityRegistry
}

// NewMCPTool creates a tool wrapper that delegates execution to an MCP server.
func NewMCPTool(serverName, toolName, description string, parameters map[string]interface{}, capReg *CapabilityRegistry) *MCPTool {
	return &MCPTool{
		BaseTool: tools.BaseTool{
			SchemaVal: tools.Schema{
				Name:        fmt.Sprintf("mcp_%s_%s", serverName, toolName),
				Description: fmt.Sprintf("[MCP:%s] %s", serverName, description),
				Parameters:  parameters,
			},
			PermLevel:         tools.PermExecute,
			HasSideEffects:    true,
			MaxOutputChars:    8000,
			ToolCategory:      tools.CategorySkill,
			IsConcurrencySafe: false,
		},
		serverName: serverName,
		capReg:     capReg,
	}
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	setID := "mcp:" + t.serverName
	set, ok := t.capReg.GetSet(setID)
	if !ok {
		return tools.Result{Error: fmt.Sprintf("MCP server %q not found", t.serverName)}
	}
	if set.Health != HealthOK {
		var entry ServerEntry
		if err := json.Unmarshal(set.Config, &entry); err != nil {
			return tools.Result{Error: fmt.Sprintf("MCP server %q config parse: %v", t.serverName, err)}
		}
		if err := t.capReg.ConnectMCPServer(setID, entry); err != nil {
			return tools.Result{Error: fmt.Sprintf("MCP server %q not connected: %v", t.serverName, err)}
		}
	}

	// Derive the original tool name from the schema (strip "mcp_<server>_" prefix).
	// Avoids a redundant ListTools RPC round-trip on every execution.
	prefix := "mcp_" + t.serverName + "_"
	origName := strings.TrimPrefix(t.SchemaVal.Name, prefix)

	// Verify the server is still connected before the call.
	set, ok = t.capReg.GetSet(setID)
	if !ok || set.Health != HealthOK {
		return tools.Result{Error: fmt.Sprintf("MCP server %q connection lost", t.serverName)}
	}

	result := t.capReg.Execute(ctx, origName, args)
	return result
}
