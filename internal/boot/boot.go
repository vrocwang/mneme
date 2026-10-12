// Package boot wires all capability sources and post-bootstrap tool registrations.
// It exists outside the capability package to avoid circular imports: mcp/setup
// imports capability, so capability cannot import mcp/setup. By placing the wiring
// in a third package, both can be imported without cycles.
package boot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	agenttoml "github.com/simon/mneme/internal/agent/toml"
	"github.com/simon/mneme/internal/bundle"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/cron"
	"github.com/simon/mneme/internal/dag"
	"github.com/simon/mneme/internal/inference"
	mcpregistry "github.com/simon/mneme/internal/mcp/registry"
	mcpstore "github.com/simon/mneme/internal/mcp/store"
	mcptools "github.com/simon/mneme/internal/mcp/tools"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/memory/conversations"
	memtools "github.com/simon/mneme/internal/memory/tools"
	"github.com/simon/mneme/internal/monitor"
	"github.com/simon/mneme/internal/security"
	"github.com/simon/mneme/internal/threads"
	"github.com/simon/mneme/internal/tools"
	ws "github.com/simon/mneme/internal/workspace"
)

// extensionCacheDir is where runtime extensions' handshake records live: a
// private subdirectory of the workspace data dir, so the cache is per-workspace,
// survives restarts, and never mixes with the extension directories it
// describes. An empty workspace disables the cache (every extension is eager).
func extensionCacheDir(workspace string) string {
	if workspace == "" {
		return ""
	}
	return filepath.Join(ws.DataDir(workspace), "extension-cache")
}

// BootstrapAll calls capability.Bootstrap then registers all tools that can be
// wired statically (no runtime dependencies like DB connections or pipelines).
// Tools needing runtime deps (threads, memory) are registered in app.go.
func BootstrapAll(reg *capability.CapabilityRegistry, workspace string, securityTier string, mcpServers []capability.ServerEntry, db *sql.DB, cfg *config.Config, mcpStore *mcpstore.Store, audit *security.AuditLogger, log *slog.Logger) {
	// Register the first-layer bundles (builtin agents + core/network/
	// productivity tools) before user-agent overrides and other capability
	// sources. This is the no-privileged-core composition step.
	bundleDeps := &bundle.Deps{
		Reg:          reg,
		Cfg:          cfg,
		Workspace:    workspace,
		SecurityTier: securityTier,
		Log:          log,
		Audit:        audit,
		BraveAPIKey:  cfg.Search.BraveAPIKey,
		TavilyAPIKey: cfg.Search.TavilyAPIKey,
		SearxngURL:   cfg.Search.SearxNGURL,
		ProxyConfig:  cfg.Proxy,
	}
	if _, err := bundle.RegisterBuiltin(context.Background(), bundleDeps); err != nil {
		log.Error("builtin bundle registration failed", "error", err)
	}

	br := bundle.NewRegistry(cfg.Bundles.Disabled)

	// cfg.Bundles.Disabled gates both the in-process bundles (through br below
	// and bundle.RegisterBuiltin) and the runtime extensions: the same list is
	// handed to capability.Bootstrap so `disabled = ["todos"]` skips the todos
	// extension too, not just in-process bundle ids.
	//
	// The [extensions] section decides whether an extension with a warm
	// handshake cache is declared (process starts on first use) or started
	// eagerly, and how long an idle lazy process may live. The cache directory
	// is derived here from the workspace's data dir: the layout is owned by
	// internal/workspace, and capability never guesses at it.
	capability.Bootstrap(reg, workspace, mcpServers, cfg.LoadExtensionConfig, cfg.Bundles.Disabled,
		capability.ExtensionLoadOptions{
			Lazy:        cfg.Extensions.Lazy,
			IdleTimeout: time.Duration(cfg.Extensions.IdleTimeoutSecs) * time.Second,
			CacheDir:    extensionCacheDir(workspace),
		}, log)

	// Skill installation tool (agent-callable, from chat).
	if br.IsEnabled(bundle.BundleSkills) {
		capability.RegisterSetTools(reg, capability.SetSkills, "Skills",
			capability.NewSkillInstallTool(ws.SkillsDir(workspace), reg),
		)
	}

	// Workflow tools (WORKFLOW.md authoring + run execution) are provided by
	// the "workflows" extension; the core keeps only the read-only document
	// layer (internal/workflow_doc) that feeds prompt guidance.

	// Config introspection tools (read-only: snapshot, autonomy, data paths).
	if br.IsEnabled(bundle.BundleConfig) {
		registerConfigTools(reg, cfg)
	}

	// Todo / task-board tools are registered by AppCore.Init, which owns the
	// store so the Wails todo RPCs and the agent tools share one instance.

	// Load user-defined agent definitions from workspace/agents/*.toml.
	// User agents override built-in ones with the same ID, and are grouped
	// into their own "user-agents" capability set rather than the Core set.
	agentsDir := ws.AgentsDir(workspace)
	userDefs, errs := agenttoml.LoadAgentsFromDir(agentsDir)
	for _, def := range userDefs {
		if _, exists := reg.GetAgent(def.ID); exists {
			reg.UnregisterAgent(def.ID)
		}
		reg.RegisterAgentSet(capability.UserAgentsSetID, capability.KindUserAgent, capability.UserAgentsSetName, def)
	}
	for _, err := range errs {
		log.Warn("agent file parse error", "error", err)
	}

	// Load agent packs from extensions/agents/ subdirectories (agent.toml + prompt.md).
	// Each pack is registered as its own capability set (agent-pack:<name>).
	agentPacksDirs := []string{
		filepath.Join(ws.ExtensionsDir(workspace), "agents"),
		filepath.Join(ws.DirExtensions, "agents"), // development: relative to CWD
	}
	for _, dir := range agentPacksDirs {
		packDefs, packErrs := agenttoml.LoadAgentPacksFromDir(dir)
		for _, def := range packDefs {
			if _, exists := reg.GetAgent(def.ID); exists {
				reg.UnregisterAgent(def.ID)
			}
			reg.RegisterAgentSet(capability.AgentPackSetID(def.ID), capability.KindAgentPack, "Agent Pack: "+def.Name, def)
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
			Store:    mcpStore,
			Log:      log,
		})
	}

	// Doctor: provided by the "doctor" extension (tools/doctor_run).

	// File-state snapshot/diff tools are provided by the "file-state" extension.

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
		setID := capability.MCPSetID(s.QualifiedName)
		entry := s.ToServerEntry()
		set := capability.NewMCPServerSet(entry)
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
			capability.RegisterSetTools(reg, capability.SetMemory, "Memory",
				newMemoryDiffTool(memory.NewMemoryDiff(s)),
			)
		}
	}
	if pipeline != nil && provider != nil && br.IsEnabled(bundle.BundleMemory) {
		capability.RegisterSetTools(reg, capability.SetMemory, "Memory",
			memtools.NewSmartWalkTool(pipeline, provider, defaultModel),
		)
	}
}

// RegisterPostBootstrapTools registers tools that depend on objects created
// late in the startup sequence (cron scheduler, monitor). br gates each domain
// bundle.
func RegisterPostBootstrapTools(reg *capability.CapabilityRegistry, sched *cron.Scheduler, mon *monitor.Manager, br *bundle.Registry) {
	if sched != nil && br.IsEnabled(bundle.BundleCron) {
		cron.RegisterTools(reg, sched)
	}
	// Desktop automation as an agent tool is provided by the desktop-auto
	// extension; the core keeps only the companion and screen-intel loops.
	if mon != nil && br.IsEnabled(bundle.BundleMonitor) {
		capability.RegisterSetTools(reg, capability.SetMonitor, "Monitor",
			monitor.NewMonitorStartTool(mon),
			monitor.NewMonitorListTool(mon),
			monitor.NewMonitorReadTool(mon),
			monitor.NewMonitorStopTool(mon),
		)
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
