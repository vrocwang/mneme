package main

// app.go is a thin Wails proxy. All business logic lives in boot.AppCore
// and individual RPC objects. This file contains only:
//   - Composition root (struct + constructor + lifecycle)
//   - Dependency injection (wireRPC)
//   - Getters/Setters for Wails RPC binding
//
// RPC delegate methods are in app_rpc.go.

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"

	_ "github.com/simon/mneme/internal/sqlite"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/app_state"
	"github.com/simon/mneme/internal/approval"
	"github.com/simon/mneme/internal/boot"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/cost"
	"github.com/simon/mneme/internal/cron"
	"github.com/simon/mneme/internal/desktop"
	"github.com/simon/mneme/internal/health"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/learning"
	mcpaudit "github.com/simon/mneme/internal/mcp/audit"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/monitor"
	"github.com/simon/mneme/internal/registry"
	"github.com/simon/mneme/internal/routing"
	"github.com/simon/mneme/internal/webhooks"
	"github.com/simon/mneme/pkg/events"
)

// App is a thin Wails proxy. All business logic lives in boot.AppCore
// and individual RPC objects.
type App struct {
	*boot.AppCore

	ctx context.Context
	gui *boot.GUIComponents

	// Wails RPC objects (wired by main.go before startup)
	capRPC      *capability.CapabilityRPC
	appStateRPC *app_state.AppStateRPC
	agentRPC    *agent.AgentRPC
	approvalRPC *approval.ApprovalRPC
	mcpRegRPC   *registry.RPC

	// Event subscriptions (managed by subscribers.go)
	eventSubs *subscriberSet
}

func NewApp(cfg *config.Config, log *slog.Logger) *App {
	return &App{
		AppCore: boot.NewAppCore(cfg, log),
	}
}

// startup is the Wails lifecycle hook. It delegates all initialization
// to boot.AppCore.Init and boot.BootstrapGUI.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.AppCore.Init(false)
	a.wireRPC()
	a.gui = boot.BootstrapGUI(a.AppCore, ctx, a)
	a.eventSubs = a.registerSubscribers()
	if a.EventBus != nil {
		a.EventBus.PublishTyped(events.DomainSystem, events.KindSystemStartup, nil)
	}
}

// shutdown is the Wails lifecycle hook. It delegates all cleanup
// to boot.ShutdownGUI and boot.AppCore.Shutdown.
func (a *App) shutdown(ctx context.Context) {
	if a.EventBus != nil {
		a.EventBus.PublishTyped(events.DomainSystem, events.KindSystemShutdown, nil)
	}
	boot.ShutdownGUI(a.gui)
	if a.eventSubs != nil {
		a.eventSubs.unsubscribeAll()
	}
	a.AppCore.Shutdown()
}

// wireRPC injects dependencies into RPC objects. This is pure DI -
// no business logic, no component creation.
func (a *App) wireRPC() {
	if a.appStateRPC != nil {
		a.appStateRPC.SetCapReg(a.CapReg)
		a.appStateRPC.SetProvider(a.Provider)
		a.appStateRPC.SetDB(a.DB)
	}
	if a.capRPC != nil {
		a.capRPC.SetRegistry(a.CapReg)
		a.capRPC.SetMCPPersister(a.McpStore)
		a.capRPC.SetLegacyExtensionsDir(filepath.Join(a.Cfg.Workspace, "extensions"))
		a.capRPC.SetSkillsDir(filepath.Join(a.Cfg.Workspace, "skills"))
	}
	if a.mcpRegRPC != nil {
		a.mcpRegRPC.Wire(a.CapReg, a.McpStore)
	}
	if a.agentRPC != nil {
		a.agentRPC.SetRegistry(a.CapReg)
	}
	if a.approvalRPC != nil {
		a.approvalRPC.SetGate(a.ApprovalGate)
	}
	if a.ApprovalGate != nil {
		a.ApprovalGate.SetEventPublisher(&approvalEventBridge{busFn: func() *events.Bus { return a.EventBus }})
	}
	if a.DB != nil && a.CapReg != nil {
		if mcpAudit, err := mcpaudit.NewLogger(a.DB, a.Log); err != nil {
			a.Log.Error("startup error", "subsystem", "mcp audit", "error", err)
		} else {
			a.CapReg.SetMCPAuditLogger(mcpAudit)
		}
	}
}

// ── Getters for Wails RPC binding ───────────────────────────────────

func (a *App) GetCapReg() *capability.CapabilityRegistry { return a.CapReg }
func (a *App) SetCapRPC(rpc *capability.CapabilityRPC)   { a.capRPC = rpc }
func (a *App) SetAppStateRPC(rpc *app_state.AppStateRPC) { a.appStateRPC = rpc }
func (a *App) SetApprovalRPC(rpc *approval.ApprovalRPC)  { a.approvalRPC = rpc }
func (a *App) GetApprovalGate() *approval.Gate           { return a.ApprovalGate }
func (a *App) GetLearningEngine() *learning.Engine       { return a.Learning }
func (a *App) GetCronScheduler() *cron.Scheduler         { return a.Cron }
func (a *App) GetMemoryPipeline() *memory.Pipeline       { return a.Pipeline }
func (a *App) GetContext() context.Context               { return a.ctx }
func (a *App) GetCompanionLoop() *desktop.CompanionLoop {
	if a.gui != nil {
		return a.gui.CompanionLoop
	}
	return nil
}
func (a *App) GetThreadStore() *conversations.Store { return a.ConvStore }
func (a *App) GetProvider() inference.Provider      { return a.Provider }
func (a *App) GetDB() *sql.DB                       { return a.DB }
func (a *App) GetRegistryRPC() *registry.RPC        { return a.mcpRegRPC }
func (a *App) SetRegistryRPC(rpc *registry.RPC)     { a.mcpRegRPC = rpc }
func (a *App) SetAgentRPC(rpc *agent.AgentRPC)      { a.agentRPC = rpc }
func (a *App) GetSessionDB() *agent.SessionDB       { return a.SessionDB }
func (a *App) GetMonitorManager() *monitor.Manager  { return a.MonitorMgr }
func (a *App) GetMetricsRegistry() *health.Registry { return a.Metrics }
func (a *App) GetCostTracker() *cost.Tracker        { return a.CostTracker }
func (a *App) GetChatService() *agent.ChatService   { return a.ChatService }
func (a *App) GetWebhookTM() *webhooks.TunnelManager {
	if a.gui != nil {
		return a.gui.WebhookTM
	}
	return nil
}

// onConfigReload delegates to boot.ReloadConfig and boot.ReloadEino.
func (a *App) onConfigReload() {
	boot.ReloadConfig(a.Cfg, a.Learning, a.Log,
		func(p inference.Provider) { a.Provider = p },
		func(r *routing.Router) { a.ModelRouter = r },
	)
	boot.ReloadEino(a.AppCore)
}
