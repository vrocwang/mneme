// Package boot wires all capability sources and post-bootstrap tool registrations.
// It exists outside the capability package to avoid circular imports: mcp/setup
// imports capability, so capability cannot import mcp/setup. By placing the wiring
// in a third package, both can be imported without cycles.
package boot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	agenttoml "github.com/simon/mneme/internal/agent/toml"
	"github.com/simon/mneme/internal/agent_workflows"
	"github.com/simon/mneme/internal/bundle"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/connectivity"
	"github.com/simon/mneme/internal/cron"
	"github.com/simon/mneme/internal/dag"
	"github.com/simon/mneme/internal/desktop"
	"github.com/simon/mneme/internal/devices"
	"github.com/simon/mneme/internal/doctor"
	"github.com/simon/mneme/internal/file_state"
	"github.com/simon/mneme/internal/http_host"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/integration"
	mcpregistry "github.com/simon/mneme/internal/mcp/registry"
	mcpstore "github.com/simon/mneme/internal/mcp/store"
	mcptools "github.com/simon/mneme/internal/mcp/tools"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/memory/conversations"
	memtools "github.com/simon/mneme/internal/memory/tools"
	"github.com/simon/mneme/internal/monitor"
	"github.com/simon/mneme/internal/threads"
	"github.com/simon/mneme/internal/todos"
	"github.com/simon/mneme/internal/tools"
	"github.com/simon/mneme/internal/workflows"
)

// BootstrapAll calls capability.Bootstrap then registers all tools that can be
// wired statically (no runtime dependencies like DB connections or pipelines).
// Tools needing runtime deps (threads, memory) are registered in app.go.
func BootstrapAll(reg *capability.CapabilityRegistry, workspace string, securityTier string, mcpServers []capability.ServerEntry, db *sql.DB, cfg *config.Config, mcpStore *mcpstore.Store, log *slog.Logger) {
	// Register optional integration RPC bindings (Composio etc.) before
	// the Wails Bind list is built. Each integration calls
	// capability.RegisterWailsRPC to add its Wails-bound types.

	// Register the first-layer bundles (builtin agents + core/network/
	// productivity tools) before user-agent overrides and other capability
	// sources. This is the no-privileged-core composition step.
	bundleDeps := &bundle.Deps{
		Reg:          reg,
		Cfg:          cfg,
		Workspace:    workspace,
		SecurityTier: securityTier,
		Log:          log,
		BraveAPIKey:  cfg.Search.BraveAPIKey,
		TavilyAPIKey: cfg.Search.TavilyAPIKey,
		SearxngURL:   cfg.Search.SearxNGURL,
		ProxyConfig:  cfg.Proxy,
	}
	if _, err := bundle.RegisterBuiltin(context.Background(), bundleDeps); err != nil {
		log.Error("builtin bundle registration failed", "error", err)
	}

	br := bundle.NewRegistry(cfg.Bundles.Disabled)

	capability.Bootstrap(reg, workspace, mcpServers, log)

	if br.IsEnabled(bundle.BundleWorkflows) {
		workflows.RegisterAll(reg, workspace, log)
	}

	// Skill installation tool (agent-callable, from chat).
	if br.IsEnabled(bundle.BundleSkills) {
		reg.EnsureSet(&capability.CapabilitySet{
			ID: "skills", Name: "Skills", Kind: capability.KindBuiltin, Enabled: true,
		})
		reg.RegisterTool("skills", capability.NewSkillInstallTool(filepath.Join(workspace, "skills"), reg))
	}

	// Agent-accessible workflow discovery tools.
	if br.IsEnabled(bundle.BundleWorkflows) {
		agent_workflows.RegisterTools(reg, filepath.Join(workspace, "workflows"), workspace)
	}

	// Config introspection tools (read-only: snapshot, autonomy, data paths).
	if br.IsEnabled(bundle.BundleConfig) {
		registerConfigTools(reg, cfg)
	}

	// Todo / task-board tools.
	if br.IsEnabled(bundle.BundleTodos) {
		todos.RegisterTools(reg, todos.NewStore(workspace))
	}

	// Load user-defined agent definitions from workspace/agents/*.toml.
	// User agents override built-in ones with the same ID, and are grouped
	// into their own "user-agents" capability set rather than the Core set.
	agentsDir := filepath.Join(workspace, "agents")
	userDefs, errs := agenttoml.LoadAgentsFromDir(agentsDir)
	for _, def := range userDefs {
		if _, exists := reg.GetAgent(def.ID); exists {
			reg.UnregisterAgent(def.ID)
		}
		reg.RegisterAgentSet("user-agents", capability.KindUserAgent, "User Agents", def)
	}
	for _, err := range errs {
		log.Warn("agent file parse error", "error", err)
	}

	// Load agent packs from extensions/agents/ subdirectories (agent.toml + prompt.md).
	// Each pack is registered as its own capability set (agent-pack:<name>).
	agentPacksDirs := []string{
		filepath.Join(workspace, "extensions", "agents"),
		filepath.Join("extensions", "agents"), // development: relative to CWD
	}
	for _, dir := range agentPacksDirs {
		packDefs, packErrs := agenttoml.LoadAgentPacksFromDir(dir)
		for _, def := range packDefs {
			if _, exists := reg.GetAgent(def.ID); exists {
				reg.UnregisterAgent(def.ID)
			}
			reg.RegisterAgentSet("agent-pack:"+def.ID, capability.KindAgentPack, "Agent Pack: "+def.Name, def)
		}
		for _, err := range packErrs {
			log.Warn("agent pack parse error", "dir", dir, "error", err)
		}
	}

	// Tier hierarchy validation removed — eino uses AgentAsTool delegation

	// ── Wired modules (parallel OpenHuman's all.rs controller registry) ──

	// MCP server management tools (agent-callable).
	if br.IsEnabled(bundle.BundleMCP) {
		mcpRegistryClient := mcpregistry.NewClient(mcpStore)
		mcptools.RegisterTools(reg, mcptools.Deps{
			Reg:      reg,
			Registry: mcpRegistryClient,
			Log:      log,
		})
	}

	// Doctor: workspace diagnostics tool.
	if br.IsEnabled(bundle.BundleDoctor) {
		doctor.RegisterTools(reg, workspace)
	}

	// File state: snapshot and diff tools for workspace change tracking.
	if br.IsEnabled(bundle.BundleFileState) {
		file_state.RegisterTools(reg)
	}

	// Connectivity: network diagnostics RPC.
	connectivity.Register()

	// Devices: mobile device pairing and tunnel management RPC.
	devices.Register(log)

	// HTTP host: ad-hoc static file serving RPC.
	http_host.Register(log)

	// DAG: lightweight multi-step automation engine.
	if db != nil && br.IsEnabled(bundle.BundleDAG) {
		dagStore, err := dag.NewStore(db)
		if err != nil {
			log.Warn("dag store init failed, DAG tools unavailable", "error", err)
		} else {
			// Runner and tools wired later in app_core.go (needs BackgroundRunner).
			dag.RegisterTools(reg, nil, dagStore)
			log.Info("dag store initialised")
		}
	}
}

// ReconnectPersistedServers connects to all enabled MCP servers stored in the
// SQLite database. Called at boot time after the registry is fully wired.
// Per-server failures are logged and do not block boot.
func ReconnectPersistedServers(reg *capability.CapabilityRegistry, mcpStore *mcpstore.Store, log *slog.Logger) {
	if mcpStore == nil {
		return
	}
	servers, err := mcpStore.ListServers()
	if err != nil {
		log.Warn("mcp boot: list persisted servers failed", "error", err)
		return
	}
	if len(servers) == 0 {
		return
	}
	log.Info("mcp boot: reconnecting persisted servers", "count", len(servers))
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		setID := "mcp:" + s.QualifiedName
		entry := s.ToServerEntry()
		entryJSON, err := json.Marshal(entry)
		if err != nil {
			log.Warn("mcp boot: marshal server entry failed", "name", s.QualifiedName, "error", err)
			entryJSON = nil
		}
		set := &capability.CapabilitySet{
			ID: setID, Name: s.QualifiedName, Kind: capability.KindMCPServer,
			Description: fmt.Sprintf("MCP server: %s (%s)", s.QualifiedName, s.Transport),
			Config:      entryJSON, Enabled: true, Health: capability.HealthUnknown,
		}
		if err := reg.AddSet(set); err != nil {
			log.Warn("mcp boot: add set failed", "name", s.QualifiedName, "error", err)
			continue
		}
		if err := reg.ConnectMCPServer(setID, entry); err != nil {
			reg.UpdateSetHealth(setID, capability.HealthDown)
			log.Warn("mcp boot: connect failed", "name", s.QualifiedName, "error", err)
			continue
		}
		log.Info("mcp boot: connected", "name", s.QualifiedName, "tools", set.ToolCount)
	}
}

// WireIntegrations registers integration-level providers (OAuth, sync)
// into the integration registry. Called after BootstrapAll when the
// capability registry is fully initialised.
func WireIntegrations(capReg *capability.CapabilityRegistry, intReg integration.IntegrationRegistry) {
	if capReg == nil || intReg == nil {
		return
	}
	// Integration providers (OAuth, sync) are registered via extensions
	// (e.g. extensions/composio/). No core registration needed.
}

// RegisterLateTools registers tools that need runtime objects created after
// BootstrapAll: threads (conversations.Store), memory (pipeline), and
// SmartWalk (pipeline + provider). br gates each domain bundle.
func RegisterLateTools(reg *capability.CapabilityRegistry, convStore *conversations.Store, pipeline *memory.Pipeline, provider inference.Provider, defaultModel string, br *bundle.Registry) {
	if convStore != nil && br.IsEnabled(bundle.BundleThreads) {
		threads.RegisterTools(reg, threads.NewOps(convStore))
	}
	if pipeline != nil && br.IsEnabled(bundle.BundleMemory) {
		capability.RegisterMemoryTools(reg, pipeline)
		// MemoryDiffTool: checkpoint-based snapshot/diff for tracking
		// memory changes between agent turns.
		if s := pipeline.Store(); s != nil {
			reg.EnsureSet(&capability.CapabilitySet{
				ID: "memory", Name: "Memory", Kind: capability.KindBuiltin, Enabled: true,
			})
			reg.RegisterTool("memory", newMemoryDiffTool(memory.NewMemoryDiff(s)))
		}
	}
	if pipeline != nil && provider != nil && br.IsEnabled(bundle.BundleMemory) {
		reg.EnsureSet(&capability.CapabilitySet{
			ID: "memory", Name: "Memory", Kind: capability.KindBuiltin, Enabled: true,
		})
		reg.RegisterTool("memory", memtools.NewSmartWalkTool(pipeline, provider, defaultModel))
	}
}

// RegisterPostBootstrapTools registers tools that depend on objects created
// late in the startup sequence (cron scheduler, desktop automator, monitor).
// br gates each domain bundle.
func RegisterPostBootstrapTools(reg *capability.CapabilityRegistry, sched *cron.Scheduler, automator *desktop.Automator, mon *monitor.Manager, br *bundle.Registry) {
	if sched != nil && br.IsEnabled(bundle.BundleCron) {
		cron.RegisterTools(reg, sched)
	}
	if automator != nil && br.IsEnabled(bundle.BundleDesktop) {
		reg.EnsureSet(&capability.CapabilitySet{
			ID: "desktop", Name: "Desktop Automation", Kind: capability.KindBuiltin, Enabled: true,
		})
		reg.RegisterTool("desktop", desktop.NewDesktopAutomateTool(automator))
	}
	if mon != nil && br.IsEnabled(bundle.BundleMonitor) {
		reg.EnsureSet(&capability.CapabilitySet{
			ID: "monitor", Name: "Monitor", Kind: capability.KindBuiltin, Enabled: true,
		})
		reg.RegisterTool("monitor", monitor.NewMonitorStartTool(mon))
		reg.RegisterTool("monitor", monitor.NewMonitorListTool(mon))
		reg.RegisterTool("monitor", monitor.NewMonitorReadTool(mon))
		reg.RegisterTool("monitor", monitor.NewMonitorStopTool(mon))
	}
}

// newMemoryDiffTool wraps memory.MemoryDiff as a tools.Tool adapter.
// Lives in boot to avoid a circular import (memory → tools → memory).
func newMemoryDiffTool(diff *memory.MemoryDiff) tools.Tool {
	return &memoryDiffAdapter{diff: diff}
}

type memoryDiffAdapter struct {
	diff   *memory.MemoryDiff
	lastCP *memory.Checkpoint
}

func (t *memoryDiffAdapter) Schema() tools.Schema {
	return tools.Schema{
		Name:        "memory_diff",
		Description: "Compare memory snapshots to see what new information has been added by sync sources.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "snapshot (capture current state) or diff (compare against last snapshot).",
					"enum":        []string{"snapshot", "diff", "list"},
				},
				"label": map[string]interface{}{
					"type":        "string",
					"description": "Label for the snapshot (only used with action=snapshot).",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *memoryDiffAdapter) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	action, _ := args["action"].(string)
	switch action {
	case "list":
		cp, err := t.diff.CreateCheckpoint("query-" + time.Now().UTC().Format("2006-01-02T15:04"))
		if err != nil {
			return tools.Result{Error: fmt.Sprintf("query memory: %v", err)}
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Current memory state: %d chunks\n", len(cp.ChunkIDs)))
		if t.lastCP != nil {
			b.WriteString(fmt.Sprintf("Last snapshot: %q (%d chunks, %s ago)\n",
				t.lastCP.Label, len(t.lastCP.ChunkIDs),
				time.Since(t.lastCP.CreatedAt).Round(time.Second)))
			b.WriteString("Use action=diff to compare against the last snapshot.")
		} else {
			b.WriteString("No prior snapshot. Use action=snapshot to create a baseline.")
		}
		return tools.Result{Success: true, Output: b.String()}
	case "snapshot":
		label, _ := args["label"].(string)
		if label == "" {
			label = fmt.Sprintf("snapshot-%d", time.Now().Unix())
		}
		cp, err := t.diff.CreateCheckpoint(label)
		if err != nil {
			return tools.Result{Error: fmt.Sprintf("create snapshot: %v", err)}
		}
		t.lastCP = cp
		return tools.Result{Success: true, Output: fmt.Sprintf("Snapshot %q captured with %d chunks.", cp.Label, len(cp.ChunkIDs))}
	case "diff":
		if t.lastCP == nil {
			return tools.Result{Error: "no snapshot available — run action=snapshot first"}
		}
		current, err := t.diff.CreateCheckpoint("current")
		if err != nil {
			return tools.Result{Error: fmt.Sprintf("create current snapshot: %v", err)}
		}
		result, err := t.diff.Diff(t.lastCP, current)
		if err != nil {
			return tools.Result{Error: fmt.Sprintf("diff: %v", err)}
		}
		return tools.Result{Success: true, Output: memory.FormatDiffReport(result)}
	default:
		return tools.Result{Error: "unknown action: " + action + " — use 'snapshot' or 'diff'"}
	}
}
