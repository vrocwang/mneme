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

	capability.Bootstrap(reg, workspace, securityTier, mcpServers, cfg.Search.BraveAPIKey, cfg.Search.TavilyAPIKey, cfg.Search.SearxNGURL, cfg.Proxy, cfg, log)

	workflows.RegisterAll(reg, workspace, log)

	// Skill installation tool (agent-callable, from chat).
	reg.RegisterTool("builtin", capability.NewSkillInstallTool(filepath.Join(workspace, "skills"), reg))

	// Agent-accessible workflow discovery tools.
	agent_workflows.RegisterTools(reg, filepath.Join(workspace, "workflows"), workspace)

	// Config introspection tools (read-only: snapshot, autonomy, data paths).
	registerConfigTools(reg, cfg)

	// Todo / task-board tools.
	todos.RegisterTools(reg, todos.NewStore(workspace))

	// Load user-defined agent definitions from workspace/agents/*.toml.
	// User agents override built-in ones with the same ID.
	agentsDir := filepath.Join(workspace, "agents")
	userDefs, errs := agenttoml.LoadAgentsFromDir(agentsDir)
	for _, def := range userDefs {
		if _, exists := reg.GetAgent(def.ID); exists {
			reg.UnregisterAgent(def.ID)
		}
		reg.RegisterAgent("builtin", def)
	}
	for _, err := range errs {
		log.Warn("agent file parse error", "error", err)
	}

	// Load agent packs from extensions/agents/ subdirectories (agent.toml + prompt.md).
	// Agent packs use the same tier/override semantics as user-defined agents.
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
			reg.RegisterAgent("builtin", def)
		}
		for _, err := range packErrs {
			log.Warn("agent pack parse error", "dir", dir, "error", err)
		}
	}

	// Validate agent tier hierarchy after all agents are loaded.
	allAgents := reg.AllAgents()
	agentDefs := make([]*tools.AgentDef, 0, len(allAgents))
	for _, desc := range allAgents {
		if def, ok := reg.GetAgent(desc.ID); ok {
			agentDefs = append(agentDefs, def)
		}
	}
	// Tier hierarchy validation removed — eino uses AgentAsTool delegation

	// ── Wired modules (parallel OpenHuman's all.rs controller registry) ──

	// MCP server management tools (agent-callable).
	mcpRegistryClient := mcpregistry.NewClient(mcpStore)
	mcptools.RegisterTools(reg, mcptools.Deps{
		Reg:      reg,
		Registry: mcpRegistryClient,
		Log:      log,
	})

	// Doctor: workspace diagnostics tool.
	doctor.RegisterTools(reg, workspace)

	// File state: snapshot and diff tools for workspace change tracking.
	file_state.RegisterTools(reg)

	// Connectivity: network diagnostics RPC.
	connectivity.Register()

	// Devices: mobile device pairing and tunnel management RPC.
	devices.Register(log)

	// HTTP host: ad-hoc static file serving RPC.
	http_host.Register(log)

	// DAG: lightweight multi-step automation engine.
	if db != nil {
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
// SmartWalk (pipeline + provider).
func RegisterLateTools(reg *capability.CapabilityRegistry, convStore *conversations.Store, pipeline *memory.Pipeline, provider inference.Provider, defaultModel string) {
	if convStore != nil {
		threads.RegisterTools(reg, threads.NewOps(convStore))
	}
	if pipeline != nil {
		capability.RegisterMemoryTools(reg, pipeline)
		// MemoryDiffTool: checkpoint-based snapshot/diff for tracking
		// memory changes between agent turns.
		if s := pipeline.Store(); s != nil {
			reg.RegisterTool("builtin", newMemoryDiffTool(memory.NewMemoryDiff(s)))
		}
	}
	if pipeline != nil && provider != nil {
		reg.RegisterTool("builtin", memtools.NewSmartWalkTool(pipeline, provider, defaultModel))
	}
}

// RegisterPostBootstrapTools registers tools that depend on objects created
// late in the startup sequence (cron scheduler, desktop automator, monitor).
func RegisterPostBootstrapTools(reg *capability.CapabilityRegistry, sched *cron.Scheduler, automator *desktop.Automator, mon *monitor.Manager) {
	if sched != nil {
		cron.RegisterTools(reg, sched)
	}
	if automator != nil {
		reg.RegisterTool("builtin", desktop.NewDesktopAutomateTool(automator))
	}
	if mon != nil {
		reg.RegisterTool("builtin", monitor.NewMonitorStartTool(mon))
		reg.RegisterTool("builtin", monitor.NewMonitorListTool(mon))
		reg.RegisterTool("builtin", monitor.NewMonitorReadTool(mon))
		reg.RegisterTool("builtin", monitor.NewMonitorStopTool(mon))
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
