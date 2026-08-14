package bundle

import (
	"context"
	"log/slog"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security"
	"github.com/simon/mneme/internal/tools"
	"github.com/simon/mneme/pkg/dispose"
)

// BuiltinBundles returns the first-layer bundles in registration order: the
// built-in agent set, then the core/network/productivity tool groups. This is
// the dsh-equivalent of the "dsh-base" bundle — always present, but each group
// can be disabled via [bundles] disabled.
func BuiltinBundles() []Bundle {
	return []Bundle{
		builtinAgentsBundle(),
		coreToolsBundle(),
		networkToolsBundle(),
		productivityToolsBundle(),
	}
}

// RegisterBuiltin creates the shared builtin capability set, applies sandbox
// config, and runs the enabled first-layer bundles. It must be called before
// any user-agent override or late-tool registration that targets the builtin
// set. Returns a composed dispose for unwinding on shutdown.
func RegisterBuiltin(ctx context.Context, d *Deps) (dispose.Func, error) {
	if d == nil || d.Reg == nil || d.Cfg == nil {
		return nil, nil
	}
	// Sandbox config must be applied before any sandboxed tool executes.
	tools.SetSandboxConfig(d.Cfg.Sandbox)

	set := &capability.CapabilitySet{
		ID:          BuiltinSetID,
		Name:        "Core",
		Kind:        capability.KindBuiltin,
		Description: "Built-in capabilities (agents and tools)",
		Health:      capability.HealthOK,
		Enabled:     true,
	}
	if err := d.Reg.AddSet(set); err != nil {
		return nil, err
	}

	reg := NewRegistry(d.Cfg.Bundles.Disabled)
	return reg.Run(ctx, d, BuiltinBundles())
}

// toolBundle adapts a list of tool factories into a Bundle that registers each
// tool into the shared builtin set.
func toolBundle(id string, factories ...func(d *Deps) tools.Tool) Bundle {
	return Func(id, func(ctx context.Context, d *Deps) (dispose.Func, error) {
		for _, f := range factories {
			if t := f(d); t != nil {
				d.Reg.RegisterTool(BuiltinSetID, t)
			}
		}
		return nil, nil
	})
}

// coreToolsBundle is the minimal tool set an agent needs to be useful: file
// and shell access, git, code search/edit, and basic system helpers. Network
// access is a separate bundle.
func coreToolsBundle() Bundle {
	return toolBundle("core-tools",
		func(d *Deps) tools.Tool { return tools.NewReadFile(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewWriteFile(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewListDir(d.Workspace) },
		func(d *Deps) tools.Tool {
			return tools.NewShell(d.Workspace, security.Tier(d.SecurityTier), d.Cfg.Tools.Shell, d.Cfg.Sandbox)
		},
		func(d *Deps) tools.Tool { return tools.NewGitOps(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewApplyPatch(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewURLGuard() },
		func(d *Deps) tools.Tool { return tools.NewReadDiff(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewRunTests(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewEditFile(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewGlob(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewGrep(d.Workspace) },
		func(d *Deps) tools.Tool { return tools.NewCurrentTime() },
		func(d *Deps) tools.Tool { return tools.NewAskUser() },
		func(d *Deps) tools.Tool { return tools.NewWait() },
	)
}

// networkToolsBundle provides web search and raw HTTP access.
func networkToolsBundle() Bundle {
	return toolBundle("network-tools",
		func(d *Deps) tools.Tool {
			return tools.NewWebSearch(d.BraveAPIKey, d.TavilyAPIKey, d.SearxngURL)
		},
		func(d *Deps) tools.Tool { return tools.NewHTTPGet(d.ProxyConfig) },
		func(d *Deps) tools.Tool { return tools.NewHTTPPost(d.ProxyConfig) },
	)
}

// productivityToolsBundle provides non-essential convenience tools. It honors
// the legacy [tools] optional_tools allow/deny list inside the bundle.
func productivityToolsBundle() Bundle {
	return Func("productivity-tools", func(ctx context.Context, d *Deps) (dispose.Func, error) {
		for _, t := range collectOptionalTools(d.Workspace, d.Cfg, d.Log) {
			d.Reg.RegisterTool(BuiltinSetID, t)
		}
		return nil, nil
	})
}

// builtinAgentsBundle registers the 12 built-in specialist agents.
func builtinAgentsBundle() Bundle {
	return Func("builtin-agents", func(ctx context.Context, d *Deps) (dispose.Func, error) {
		for _, def := range builtinAgentDefs {
			d.Reg.RegisterAgent(BuiltinSetID, def)
		}
		return nil, nil
	})
}

// optionalToolFactories maps optional tool names to their constructors. These
// are non-core conveniences, disableable via [tools] optional_tools or by
// disabling the productivity-tools bundle.
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

// collectOptionalTools returns the optional tools to register based on
// [tools] optional_tools: empty = all, ["none"] = none, else only listed.
func collectOptionalTools(workspace string, cfg *config.Config, log *slog.Logger) []tools.Tool {
	requested := cfg.Tools.OptionalTools

	if len(requested) == 1 && requested[0] == "none" {
		log.Info("optional tools disabled by config")
		return nil
	}

	if len(requested) == 0 {
		out := make([]tools.Tool, 0, len(optionalToolFactories))
		for _, factory := range optionalToolFactories {
			out = append(out, factory(workspace))
		}
		return out
	}

	var out []tools.Tool
	for _, name := range requested {
		factory, ok := optionalToolFactories[name]
		if !ok {
			log.Warn("unknown optional tool in config, skipping", "tool", name)
			continue
		}
		out = append(out, factory(workspace))
		log.Info("optional tool registered", "tool", name)
	}
	return out
}

// builtinAgentDefs is the first-layer agent set. User-defined agents
// (workspace/agents/*.toml) override these by ID at boot.
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
