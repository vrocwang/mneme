// Package tools provides LLM-callable MCP server management tools.
// Thin wrappers over the capability registry's existing MCP infrastructure
// plus registry search via the Smithery.ai API.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/mcp/client"
	"github.com/simon/mneme/internal/mcp/registry"
	pkgtools "github.com/simon/mneme/pkg/tools"
)

// Deps bundles the dependencies needed by MCP management tools.
type Deps struct {
	Reg      *capability.CapabilityRegistry
	Registry *registry.Client
	Log      *slog.Logger
}

// ── mcp_list_servers ────────────────────────────────────────────────────────

type listServersTool struct{ deps Deps }

func (t *listServersTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name:        "mcp_list_servers",
		Description: "List all installed MCP servers with their status (connected/disconnected), tool count, and transport type.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *listServersTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	sets := t.deps.Reg.ListSets()
	type serverInfo struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		Health      string `json:"health"`
		Enabled     bool   `json:"enabled"`
		Description string `json:"description,omitempty"`
		ToolCount   int    `json:"tool_count"`
	}
	var servers []serverInfo
	for _, s := range sets {
		if s.Kind == capability.KindMCPServer {
			servers = append(servers, serverInfo{
				Name:        s.Name,
				Kind:        string(s.Kind),
				Health:      string(s.Health),
				Enabled:     s.Enabled,
				Description: s.Description,
				ToolCount:   s.ToolCount,
			})
		}
	}
	if len(servers) == 0 {
		return pkgtools.Result{Success: true, Output: "No MCP servers installed."}
	}
	out, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return pkgtools.Result{Success: true, Output: string(out)}
}

// ── mcp_list_tools ──────────────────────────────────────────────────────────

type listToolsTool struct{ deps Deps }

func (t *listToolsTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name:        "mcp_list_tools",
		Description: "List all tools provided by a specific MCP server. The server must be installed and connected.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"server": map[string]interface{}{
					"type":        "string",
					"description": "The name of the MCP server to list tools from.",
				},
			},
			"required": []string{"server"},
		},
	}
}

func (t *listToolsTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	server, _ := args["server"].(string)
	if server == "" {
		return pkgtools.Result{Error: "server name is required"}
	}
	setID := "mcp:" + server
	set, ok := t.deps.Reg.GetSet(setID)
	if !ok {
		return pkgtools.Result{Error: fmt.Sprintf("MCP server %q not found", server)}
	}

	allTools := t.deps.Reg.AllTools()
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var result []toolInfo
	prefix := "mcp:" + server + ":"
	for _, td := range allTools {
		if strings.HasPrefix(td.Name, prefix) {
			result = append(result, toolInfo{Name: td.Name, Description: td.Description})
		}
	}
	if len(result) == 0 {
		return pkgtools.Result{Output: fmt.Sprintf("No tools found for MCP server %q (health: %s). The server may not be connected.", server, set.Health)}
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return pkgtools.Result{Success: true, Output: string(out)}
}

// ── mcp_search ──────────────────────────────────────────────────────────────

type searchTool struct{ deps Deps }

func (t *searchTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name:        "mcp_search",
		Description: "Search for MCP servers available to install. Queries the Smithery.ai registry (with offline fallback). Leave query empty to browse all popular servers.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query. Leave empty to list all available servers.",
				},
			},
		},
	}
}

func (t *searchTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	query, _ := args["query"].(string)
	var results []registry.ServerSummary
	if t.deps.Registry != nil {
		var err error
		results, err = t.deps.Registry.Search(ctx, query)
		if err != nil {
			return pkgtools.Result{Error: fmt.Sprintf("search registry: %v", err)}
		}
	}
	type output struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Source      string `json:"source"`
	}
	out := make([]output, 0, len(results))
	for _, r := range results {
		out = append(out, output{Name: r.QualifiedName, DisplayName: r.DisplayName, Description: r.Description, Source: r.Source})
	}
	if len(out) == 0 {
		return pkgtools.Result{Success: true, Output: fmt.Sprintf("No MCP servers found for %q. Try a different query or leave empty to browse popular servers.", query)}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return pkgtools.Result{Success: true, Output: string(data)}
}

// ── mcp_test_connection ─────────────────────────────────────────────────────

type testConnTool struct{ deps Deps }

func (t *testConnTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name:        "mcp_test_connection",
		Description: "Test connection to an MCP server without installing it. Supports stdio (command+args) and http (url) transports. Returns the list of tools the server exposes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "A label for the server (used in log messages only).",
				},
				"transport": map[string]interface{}{
					"type":        "string",
					"description": "Transport type: 'stdio' for command-line tools or 'http' for URL-based servers.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Command to run (stdio transport only).",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"description": "Command-line arguments (stdio transport only).",
				},
				"url": map[string]interface{}{
					"type":        "string",
					"description": "Server URL (http transport only).",
				},
			},
			"required": []string{"name", "transport"},
		},
	}
}

func (t *testConnTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	name, _ := args["name"].(string)
	transport, _ := args["transport"].(string)
	command, _ := args["command"].(string)
	url, _ := args["url"].(string)
	var cmdArgs []string
	if arr, ok := args["args"].([]interface{}); ok {
		for _, a := range arr {
			if s, ok := a.(string); ok {
				cmdArgs = append(cmdArgs, s)
			}
		}
	}
	if name == "" || transport == "" {
		return pkgtools.Result{Error: "name and transport are required"}
	}

	var mcpClient *client.Client
	switch transport {
	case "stdio":
		if command == "" {
			return pkgtools.Result{Error: "command is required for stdio transport"}
		}
		c, err := client.NewStdio(name, command, cmdArgs...)
		if err != nil {
			return pkgtools.Result{Error: fmt.Sprintf("create client: %v", err)}
		}
		defer c.Close()
		mcpClient = c
	case "http":
		if url == "" {
			return pkgtools.Result{Error: "url is required for http transport"}
		}
		c := client.NewHTTP(name, url)
		if err := c.Initialize(ctx); err != nil {
			return pkgtools.Result{Error: fmt.Sprintf("initialize: %v", err)}
		}
		mcpClient = c
	default:
		return pkgtools.Result{Error: fmt.Sprintf("unknown transport %q; use 'stdio' or 'http'", transport)}
	}

	start := time.Now()
	mcpTools, err := mcpClient.ListTools(ctx)
	elapsed := time.Since(start)
	if err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("list tools: %v", err)}
	}
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	tools := make([]toolInfo, 0, len(mcpTools))
	for _, mt := range mcpTools {
		tools = append(tools, toolInfo{Name: mt.Name, Description: mt.Description})
	}
	data, err := json.MarshalIndent(map[string]interface{}{
		"ok":        true,
		"transport": transport,
		"tools":     tools,
		"elapsed":   elapsed.String(),
	}, "", "  ")
	if err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return pkgtools.Result{Success: true, Output: string(data)}
}

// ── mcp_install ─────────────────────────────────────────────────────────────

type installTool struct{ deps Deps }

func (t *installTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name: "mcp_install",
		Description: "Install a new MCP server. Provide a 'name' to install from the Smithery registry (recommended), or provide 'name'+'transport'+'command'/'url' for manual install. " +
			"Use mcp_search to discover available servers.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Qualified name of the MCP server. For registry installs, use the name from mcp_search results. For manual installs, any unique name.",
				},
				"transport": map[string]interface{}{
					"type":        "string",
					"description": "Transport type: 'stdio' or 'http'. Required for manual install; ignored when installing from registry.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Command to run (stdio transport, manual install only).",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"description": "Command-line arguments (stdio transport, manual install only).",
				},
				"url": map[string]interface{}{
					"type":        "string",
					"description": "Server URL (http transport, manual install only).",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *installTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	name, _ := args["name"].(string)
	if name == "" {
		return pkgtools.Result{Error: "name is required"}
	}

	setID := "mcp:" + name
	if _, ok := t.deps.Reg.GetSet(setID); ok {
		return pkgtools.Result{Error: fmt.Sprintf("server %q is already installed. Use mcp_uninstall first to reinstall.", name)}
	}

	var entry capability.ServerEntry

	// Try registry lookup first (when no explicit transport/command is given).
	manualTransport, _ := args["transport"].(string)
	if manualTransport == "" && t.deps.Registry != nil {
		resolved, err := t.deps.Registry.ResolveInstall(ctx, name)
		if err == nil {
			entry = capability.ServerEntry{
				Name: name, Transport: resolved.Transport, Command: resolved.Command,
				Args: resolved.Args, URL: resolved.DeploymentURL, Enabled: true,
			}
		}
		// If registry lookup fails, fall through to manual mode.
	}

	// Manual install when registry lookup didn't resolve.
	if entry.Transport == "" {
		transport := manualTransport
		command, _ := args["command"].(string)
		url, _ := args["url"].(string)
		var cmdArgs []string
		if arr, ok := args["args"].([]interface{}); ok {
			for _, a := range arr {
				if s, ok := a.(string); ok {
					cmdArgs = append(cmdArgs, s)
				}
			}
		}
		if transport == "" {
			return pkgtools.Result{Error: "transport is required for manual install, or use a registry name from mcp_search for automatic resolution"}
		}
		switch transport {
		case "stdio":
			if command == "" {
				return pkgtools.Result{Error: "command is required for stdio transport"}
			}
		case "http":
			if url == "" {
				return pkgtools.Result{Error: "url is required for http transport"}
			}
		default:
			return pkgtools.Result{Error: fmt.Sprintf("unknown transport %q; use 'stdio' or 'http'", transport)}
		}
		entry = capability.ServerEntry{
			Name: name, Transport: transport, Command: command, Args: cmdArgs, URL: url, Enabled: true,
		}
	}

	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("marshal server entry: %v", err)}
	}
	set := &capability.CapabilitySet{
		ID: setID, Name: name, Kind: capability.KindMCPServer,
		Description: fmt.Sprintf("MCP server: %s (%s)", name, entry.Transport), Enabled: true, Health: capability.HealthUnknown,
		Config: entryJSON,
	}
	if err := t.deps.Reg.AddSet(set); err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("install server: %v", err)}
	}

	if err := t.deps.Reg.ConnectMCPServer(setID, entry); err != nil {
		t.deps.Reg.UpdateSetHealth(setID, capability.HealthDown)
		return pkgtools.Result{Error: fmt.Sprintf("installed but connect failed: %v. Use mcp_connect to retry.", err)}
	}
	t.deps.Reg.UpdateSetHealth(setID, capability.HealthOK)
	return pkgtools.Result{Success: true, Output: fmt.Sprintf("MCP server %q installed and connected successfully.", name)}
}

// ── mcp_uninstall ───────────────────────────────────────────────────────────

type uninstallTool struct{ deps Deps }

func (t *uninstallTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name:        "mcp_uninstall",
		Description: "Uninstall an MCP server by name. Disconnects first if connected.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the MCP server to uninstall.",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *uninstallTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	name, _ := args["name"].(string)
	if name == "" {
		return pkgtools.Result{Error: "name is required"}
	}
	setID := "mcp:" + name
	_ = t.deps.Reg.DisconnectMCPServer(setID)
	if err := t.deps.Reg.RemoveSet(setID); err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("uninstall: %v", err)}
	}
	return pkgtools.Result{Success: true, Output: fmt.Sprintf("MCP server %q uninstalled.", name)}
}

// ── mcp_connect ─────────────────────────────────────────────────────────────

type connectTool struct{ deps Deps }

func (t *connectTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name:        "mcp_connect",
		Description: "Connect to an installed MCP server. Loads its tools into the agent.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the MCP server to connect.",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *connectTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	name, _ := args["name"].(string)
	if name == "" {
		return pkgtools.Result{Error: "name is required"}
	}
	setID := "mcp:" + name
	if err := t.deps.Reg.ConnectSet(setID); err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("connect: %v", err)}
	}
	return pkgtools.Result{Success: true, Output: fmt.Sprintf("MCP server %q connected.", name)}
}

// ── mcp_disconnect ──────────────────────────────────────────────────────────

type disconnectTool struct{ deps Deps }

func (t *disconnectTool) Schema() pkgtools.Schema {
	return pkgtools.Schema{
		Name:        "mcp_disconnect",
		Description: "Disconnect from an MCP server. Its tools will no longer be available.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the MCP server to disconnect.",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *disconnectTool) Execute(ctx context.Context, args map[string]interface{}) pkgtools.Result {
	name, _ := args["name"].(string)
	if name == "" {
		return pkgtools.Result{Error: "name is required"}
	}
	setID := "mcp:" + name
	if err := t.deps.Reg.DisconnectSet(setID); err != nil {
		return pkgtools.Result{Error: fmt.Sprintf("disconnect: %v", err)}
	}
	return pkgtools.Result{Success: true, Output: fmt.Sprintf("MCP server %q disconnected.", name)}
}

// ── Registration ────────────────────────────────────────────────────────────

// RegisterTools registers all MCP management tools with the capability registry
// under the "mcp" set.
func RegisterTools(reg *capability.CapabilityRegistry, deps Deps) {
	reg.EnsureSet(&capability.CapabilitySet{
		ID:      "mcp",
		Name:    "MCP Management",
		Kind:    capability.KindBuiltin,
		Enabled: true,
	})
	reg.RegisterTool("mcp", &listServersTool{deps: deps})
	reg.RegisterTool("mcp", &listToolsTool{deps: deps})
	reg.RegisterTool("mcp", &searchTool{deps: deps})
	reg.RegisterTool("mcp", &testConnTool{deps: deps})
	reg.RegisterTool("mcp", &installTool{deps: deps})
	reg.RegisterTool("mcp", &uninstallTool{deps: deps})
	reg.RegisterTool("mcp", &connectTool{deps: deps})
	reg.RegisterTool("mcp", &disconnectTool{deps: deps})
}
