package capability

import (
	"log/slog"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security"
	"github.com/simon/mneme/internal/tools"
)

// registerBuiltins registers all core tools and built-in agents.
// Non-core tools are registered via registerOptionalTools() which is
// config-driven, enabling future migration to extensions.
func registerBuiltins(reg *CapabilityRegistry, workspace string, securityTier string, braveAPIKey, tavilyAPIKey, searxngURL string, proxyConfig config.ProxyConfig, cfg *config.Config, log *slog.Logger) {
	set := &CapabilitySet{
		ID:          "builtin",
		Name:        "Core",
		Kind:        KindBuiltin,
		Description: "Built-in tools and agents",
		Health:      HealthOK,
		Enabled:     true,
	}

	tier := security.Tier(securityTier)

	// Apply sandbox config before registering any tools that use sandboxCmd.
	tools.SetSandboxConfig(cfg.Sandbox)

	// Phase 1: Core tools (always registered - essential for agent operation)
	for _, t := range []tools.Tool{
		tools.NewReadFile(workspace),
		tools.NewWriteFile(workspace),
		tools.NewListDir(workspace),
		tools.NewShell(workspace, tier, cfg.Tools.Shell, cfg.Sandbox),
		tools.NewHTTPGet(proxyConfig),
		tools.NewHTTPPost(proxyConfig),
		tools.NewWebSearch(braveAPIKey, tavilyAPIKey, searxngURL),
		tools.NewGitOps(workspace),
	} {
		reg.RegisterTool("builtin", t)
	}

	// Phase 2: Core dev tools (always registered - commonly needed by coder agent)
	for _, t := range []tools.Tool{
		tools.NewApplyPatch(workspace),
		tools.NewURLGuard(),
		tools.NewReadDiff(workspace),
		tools.NewRunTests(workspace),
	} {
		reg.RegisterTool("builtin", t)
	}

	// Phase 3: Core filesystem/system tools (always registered)
	for _, t := range []tools.Tool{
		tools.NewEditFile(workspace),
		tools.NewGlob(workspace),
		tools.NewGrep(workspace),
		tools.NewCurrentTime(),
		tools.NewAskUser(),
		tools.NewWait(),
	} {
		reg.RegisterTool("builtin", t)
	}

	// Phase 4: Optional tools (config-driven, candidates for extension migration)
	registerOptionalTools(reg, workspace, cfg, log)

	// Built-in agents
	for _, def := range builtinAgentDefs {
		reg.RegisterAgent("builtin", def)
	}

	reg.AddSet(set)
	log.Info("builtin capabilities registered", "tools", set.ToolCount, "agents", set.AgentCount)
}

// optionalToolFactories maps tool names to their constructor functions.
// These tools are non-core and can be:
//   - Enabled selectively via [tools] optional_tools = ["tool_name", ...]
//   - Disabled via [tools] optional_tools = ["none"]
//   - All enabled by default (empty config = backward compatible)
//
// Future: these tools should be moved to extensions for full decoupling.
var optionalToolFactories = map[string]func(workspace string) tools.Tool{
	"whatsapp_data":    func(ws string) tools.Tool { return tools.NewWhatsAppData(ws) },
	"detect_tools":     func(_ string) tools.Tool { return tools.NewDetectTools() },
	"run_linter":       func(ws string) tools.Tool { return tools.NewRunLinter(ws) },
	"workspace_state":  func(ws string) tools.Tool { return tools.NewWorkspaceState(ws) },
	"update_memory_md": func(ws string) tools.Tool { return tools.NewUpdateMemoryMD(ws) },
	"csv_export":       func(ws string) tools.Tool { return tools.NewCSVExport(ws) },
	"browser_open":     func(_ string) tools.Tool { return tools.NewBrowserOpen() },
	"image_info":       func(ws string) tools.Tool { return tools.NewImageInfo(ws) },
}

// registerOptionalTools registers non-core tools based on configuration.
// Default behavior (empty OptionalTools): all optional tools registered (backward compatible).
// ["none"]: no optional tools registered.
// ["tool1", "tool2"]: only the listed tools registered.
func registerOptionalTools(reg *CapabilityRegistry, workspace string, cfg *config.Config, log *slog.Logger) {
	requested := cfg.Tools.OptionalTools

	// Check for explicit disable.
	if len(requested) == 1 && requested[0] == "none" {
		log.Info("optional tools disabled by config")
		return
	}

	// Default: register all optional tools (backward compatible).
	if len(requested) == 0 {
		for _, factory := range optionalToolFactories {
			reg.RegisterTool("builtin", factory(workspace))
		}
		return
	}

	// Selective registration.
	for _, name := range requested {
		factory, ok := optionalToolFactories[name]
		if !ok {
			log.Warn("unknown optional tool in config, skipping", "tool", name)
			continue
		}
		reg.RegisterTool("builtin", factory(workspace))
		log.Info("optional tool registered", "tool", name)
	}
}

// builtinAgentDefs moved from agent/registry.go.
var builtinAgentDefs = []*tools.AgentDef{
	{
		ID: "general", Name: "General", Tier: "chat",
		Description:   "General-purpose assistant for conversation and task help",
		MaxIterations: 10, ToolAllowlist: []string{"*"},
	},
	{
		ID: "orchestrator", Name: "Orchestrator", Tier: "chat",
		Description:   "Coordinates sub-agents for complex multi-step tasks",
		MaxIterations: 20, ToolAllowlist: []string{"*"},
		SubagentRefs: []tools.SubagentRef{{AgentID: "researcher"}, {AgentID: "coder"}, {AgentID: "planner"}, {AgentID: "critic"}},
	},
	{
		ID: "researcher", Name: "Researcher", Tier: "reasoning",
		Description: "Deep research and analysis", MaxIterations: 15,
		ToolAllowlist: []string{"read_file", "web_search", "http_get", "http_post", "browser", "memory_search"},
		Model:         "reasoning",
	},
	{
		ID: "coder", Name: "Coder", Tier: "worker",
		Description: "Writes and modifies code", MaxIterations: 15,
		ToolAllowlist: []string{"read_file", "write_file", "list_dir", "shell", "grep", "glob", "run_tests", "read_diff", "lsp", "apply_patch"},
		SandboxMode:   "read_write",
	},
	{
		ID: "critic", Name: "Critic", Tier: "reasoning",
		Description: "Reviews and critiques output for quality assurance", MaxIterations: 5,
	},
	{
		ID: "planner", Name: "Planner", Tier: "reasoning",
		Description: "Creates step-by-step plans for complex tasks", MaxIterations: 10,
	},
	{
		ID: "summarizer", Name: "Summarizer", Tier: "worker",
		Description: "Produces compact summaries", MaxIterations: 3, Hidden: true,
		Model: "summary",
	},
	{
		ID: "archivist", Name: "Archivist", Tier: "worker",
		Description: "Manages memory archiving and retrieval", MaxIterations: 5, Hidden: true,
		ToolAllowlist: []string{"memory_search", "memory_save"}, Background: true,
	},
	{
		ID: "mcp_setup", Name: "MCP Setup", Tier: "worker",
		Description: "Configures MCP servers", MaxIterations: 10,
		ToolAllowlist: []string{"mcp_list_servers", "mcp_list_tools", "mcp_install", "mcp_uninstall", "mcp_connect", "mcp_disconnect"},
	},
	{
		ID: "tool_maker", Name: "Tool Maker", Tier: "worker",
		Description: "Creates new tools", MaxIterations: 15,
		ToolAllowlist: []string{"read_file", "write_file", "shell", "list_dir"},
	},
	{
		ID: "task_manager", Name: "Task Manager", Tier: "reasoning",
		Description: "Manages and dispatches tasks", MaxIterations: 15,
	},
	{
		ID: "tools_agent", Name: "Tools Agent", Tier: "worker",
		Description:   "Specializes in tool creation and integration setup",
		ToolAllowlist: []string{"read_file", "write_file", "list_dir", "shell", "http_get", "http_post"},
		MaxIterations: 15, Hidden: true,
	},
}
