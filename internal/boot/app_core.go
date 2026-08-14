package boot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/approval"
	"github.com/simon/mneme/internal/artifacts"
	"github.com/simon/mneme/internal/bundle"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/channels"
	"github.com/simon/mneme/internal/config"
	ctxmgr "github.com/simon/mneme/internal/context"
	"github.com/simon/mneme/internal/cost"
	"github.com/simon/mneme/internal/cron"
	"github.com/simon/mneme/internal/dag"
	eino "github.com/simon/mneme/internal/eino"
	einocb "github.com/simon/mneme/internal/eino/callbacks"
	einomw "github.com/simon/mneme/internal/eino/middleware"
	"github.com/simon/mneme/internal/health"
	"github.com/simon/mneme/internal/heartbeat"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/integration"
	"github.com/simon/mneme/internal/learning"
	mcpstore "github.com/simon/mneme/internal/mcp/store"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/memory/goals"
	"github.com/simon/mneme/internal/memory/profile"
	memsync "github.com/simon/mneme/internal/memory/sync"
	"github.com/simon/mneme/internal/monitor"
	"github.com/simon/mneme/internal/notifications"
	"github.com/simon/mneme/internal/observability"
	"github.com/simon/mneme/internal/prompts"
	"github.com/simon/mneme/internal/routing"
	"github.com/simon/mneme/internal/security"
	"github.com/simon/mneme/internal/skills"
	"github.com/simon/mneme/internal/subconscious"
	"github.com/simon/mneme/internal/tools"
	"github.com/simon/mneme/internal/workspace"
	"github.com/simon/mneme/pkg/events"
)

// AppCore holds all shared application state used by both GUI and CLI.
// GUI wraps this with Wails RPC methods; CLI uses it directly.
type AppCore struct {
	Cfg    *config.Config
	Log    *slog.Logger
	ObsHub *observability.Hub

	Provider inference.Provider
	CapReg   *capability.CapabilityRegistry
	McpStore *mcpstore.Store

	DB               *sql.DB
	SessionDB        *agent.SessionDB
	ConvStore        *conversations.Store
	Pipeline         *memory.Pipeline
	ProfileStore     *profile.Store
	MemoryPrefetcher *agent.MemoryPrefetcher
	SessionTracker   *ctxmgr.SessionMemoryTracker

	Cron        *cron.Scheduler
	Heartbeat   *heartbeat.Heartbeat
	Subcon      *subconscious.Engine
	Learning    *learning.Engine
	ToolTracker *learning.ToolTrackerHook
	Metrics     *health.Registry
	CostTracker *cost.Tracker

	ChatService        *agent.ChatService
	HookReg            *agent.PostTurnHookRegistry
	ChannelOrch        *channels.Orchestrator
	SkillsInjector     *agent.SkillsInjector
	ExperienceInjector *agent.ExperienceInjector
	WorkflowInjector   *agent.WorkflowInjector

	MonitorMgr *monitor.Manager
	SyncMgr    *memsync.Manager

	ModelRouter    *routing.Router
	SecurityPolicy *security.SecurityPolicy
	ApprovalGate   *approval.Gate

	EventBus    *events.Bus
	AuditLogger *security.AuditLogger
	NotifBus    *notifications.Bus

	// eino integration fields.
	Runner          *eino.Runner
	Callbacks       *einocb.Manager
	PromptMgr       *prompts.Manager
	CheckPointStore *agent.SQLiteCheckPointStore

	// DAG automation engine.
	DAGStore  *dag.Store
	DAGRunner *dag.Runner

	// Background task execution.
	BackgroundRunner *agent.BackgroundRunner
	TaskDispatcher   *agent.TaskDispatcher
	TaskStore        *agent.TaskStore

	StartupErrors []string
}

// NewAppCore creates an uninitialized application core.
func NewAppCore(cfg *config.Config, log *slog.Logger) *AppCore {
	return &AppCore{Cfg: cfg, Log: log}
}

// Init initializes all shared components. When headless is true, GUI-only
// components (desktop, channels, HTTP servers) are skipped.
func (a *AppCore) Init(headless bool) {
	a.Log.Info("app core initializing")

	// ── Observability ─────────────────────────────────────────────────
	a.ObsHub = observability.InitFromConfig(
		a.Cfg.Observability.SentryDSN,
		a.Cfg.Observability.TracingEnabled,
		a.Cfg.Observability.LogLevel,
		a.Cfg.Workspace,
		a.Log,
	)
	a.Log = a.ObsHub.Logger()

	// ── Workspace ────────────────────────────────────────────────────
	if err := workspace.Bootstrap(a.Cfg.Workspace); err != nil {
		a.Log.Error("workspace bootstrap failed", "error", err)
	}
	if err := workspace.ExtractEmbeddedExtensions(a.Cfg.Workspace); err != nil {
		a.Log.Warn("extension extraction failed", "error", err)
	}

	// ── Database ─────────────────────────────────────────────────────
	db, err := OpenDatabase(a.Cfg.Workspace)
	if err != nil {
		a.Log.Error("open database failed", "error", err)
	} else {
		a.DB = db

		auditLogger, err := security.NewAuditLogger(db, a.Log)
		if err != nil {
			a.addStartupError("audit logger", err)
		} else {
			a.AuditLogger = auditLogger
		}

		notifBus, err := notifications.NewBus(db, a.Log)
		if err != nil {
			a.addStartupError("notification bus", err)
		} else {
			a.NotifBus = notifBus
		}

		approvalStore, err := approval.NewStore(db)
		if err != nil {
			a.addStartupError("approval store", err)
		} else {
			enabled := approvalGateEnabled()
			a.ApprovalGate = approval.NewGate(approvalStore, nil, a.Log, enabled)
			if !enabled {
				a.Log.Info("approval gate disabled via MNEME_APPROVAL_GATE")
			}
		}
	}

	// ── Session DB ───────────────────────────────────────────────────
	if a.DB != nil {
		legacyDir := filepath.Join(a.Cfg.Workspace, "data", "agent_sessions")
		sessionDB, err := agent.NewSessionDB(a.DB, legacyDir)
		if err != nil {
			a.addStartupError("session DB", err)
		} else {
			a.SessionDB = sessionDB
		}
	}

	// ── CheckPoint store ────────────────────────────────────────────
	if a.DB != nil {
		cpStore, err := agent.NewSQLiteCheckPointStore(a.DB)
		if err != nil {
			a.addStartupError("checkpoint store", err)
		} else {
			a.CheckPointStore = cpStore
		}
	}
	// ── Provider ──────────────────────────────────────────────────────
	a.Provider = NewProvider(a.Cfg)
	if a.Provider != nil {
		a.Log.Info("using provider", "name", a.Provider.Name(), "model", a.Cfg.Agent.DefaultModel)
	} else {
		a.addStartupError("provider", fmt.Errorf("no provider configured"))
	}

	// ── Pipeline + conversations store (before capReg) ───────────────
	if a.DB != nil {
		// L3 persona layer: ensure the profile table exists and wire the store
		// into the middleware so extracted user facets reach the prompt.
		if err := profile.EnsureSchema(a.DB); err != nil {
			a.addStartupError("profile schema", err)
		} else {
			a.ProfileStore = profile.NewStore(a.DB)
		}

		a.Pipeline, a.ConvStore = NewPipeline(a.DB, a.Provider, a.Cfg, nil, a.Log)
		if a.ConvStore != nil {
			a.Log.Info("conversations store initialized")
		}
		if a.Pipeline != nil {
			// Wire the L3 persona store into the pipeline so L2→L3 facet
			// extraction runs during scenario aggregation.
			if a.ProfileStore != nil {
				a.Pipeline.WithProfile(a.ProfileStore)
			}
			a.SessionTracker = ctxmgr.NewSessionMemoryTracker()
			a.MemoryPrefetcher = NewMemoryPrefetcher(a.Pipeline)
		}
	} else {
		a.Log.Warn("database not available, conversations store and memory pipeline disabled")
	}

	// ── Capability registry ──────────────────────────────────────────
	a.CapReg, a.McpStore = NewCapRegistry(a.Cfg, a.DB, a.Log)

	// Register late tools now that capReg is available.
	if a.Pipeline != nil && a.CapReg != nil && a.ConvStore != nil {
		RegisterLateTools(a.CapReg, a.ConvStore, a.Pipeline, a.Provider, a.Cfg.Agent.DefaultModel, bundle.NewRegistry(a.Cfg.Bundles.Disabled))
	}

	// ── Goals store & tools ────────────────────────────────────────────
	goalsPath := filepath.Join(a.Cfg.Workspace, "data", "MEMORY_GOALS.md")
	if goalsStore, err := goals.NewStore(goalsPath); err == nil && a.CapReg != nil {
		if bundle.NewRegistry(a.Cfg.Bundles.Disabled).IsEnabled(bundle.BundleGoals) {
			goals.RegisterTools(a.CapReg, goalsStore)
			a.Log.Info("memory goals store initialized", "path", goalsPath)
		}
	} else if err != nil {
		a.Log.Warn("memory goals store init failed", "error", err)
	}

	// ── Prompt manager ───────────────────────────────────────────────
	a.PromptMgr = prompts.New(a.Cfg.Workspace)

	// ── Injectors ────────────────────────────────────────────────────
	skillsDir := filepath.Join(a.Cfg.Workspace, "skills")
	skillsReg := skills.NewRegistry()
	a.SkillsInjector = agent.NewSkillsInjector(skillsDir).WithRegistry(skillsReg)
	if err := a.SkillsInjector.Load(); err != nil {
		a.Log.Warn("failed to load skills", "dir", skillsDir, "error", err)
	}
	a.ExperienceInjector = agent.NewExperienceInjector(a.Learning)
	a.WorkflowInjector = agent.NewWorkflowInjector(
		filepath.Join(a.Cfg.Workspace, "workflows"),
		a.Cfg.Workspace,
	)
	a.WorkflowInjector.Load()

	// ── Event bus ────────────────────────────────────────────────────
	a.EventBus = events.NewBus(1024)
	capability.SetMCPEventHook(func(kind, name string, count int) {
		a.EventBus.PublishTyped(events.DomainMCP, events.EventKind(kind), map[string]interface{}{"server": name, "tools": count})
	})
	// Startup event published by caller after subscribers are registered.

	// ── Security policy ──────────────────────────────────────────────
	a.SecurityPolicy = a.Cfg.BuildSecurityPolicy()
	security.ReloadLivePolicy(a.SecurityPolicy)
	sc := a.Cfg.Security.Commands
	security.SetExtraAllowedCommands(sc.ExtraAllowedCommands)
	security.SetCommandPolicy(sc.BlockHighRisk, sc.RequireMediumApproval)
	security.SetCommandOverrides(sc.ExtraReadOnly, sc.ExtraNetwork, sc.ExtraDestructive, sc.ExtraInstall, sc.ExtraExecutors, sc.ExtraDangerousEnv)
	tools.SetToolTimeoutSecs(a.Cfg.Agent.Limits.DefaultToolTimeout)

	// ── Chat service ─────────────────────────────────────────────────
	a.ChatService = agent.NewChatService(agent.ChatServiceConfig{
		Log: a.Log,
		ResolveModel: func(msg string) string {
			return a.ModelRouter.Resolve(routing.ClassifyTaskKind(msg))
		},
		SaveUserMessage: SaveUserMessage(func() *conversations.Store { return a.ConvStore }),
	})
	a.HookReg = agent.NewPostTurnHookRegistry().WithLogger(a.Log)
	a.ChatService.SetHookRegistry(a.HookReg)

	// Post-turn hooks, cron jobs, subconscious evaluators, and the heartbeat
	// handler are registered by the event bundles after all their runtime
	// dependencies are created below (see the "event bundles" step).

	// ── Channels (GUI only) ──────────────────────────────────────────
	if !headless && a.ChatService != nil {
		// Register core channels through CapabilityRegistry.
		RegisterCoreChannels(a.CapReg, a.Log)

		a.ChannelOrch = channels.NewOrchestrator(a.Log,
			channels.NewDispatcher(a.Log, channels.DispatchToChatService(a.ChatService)))

		// Start channels from config via CapabilityRegistry.
		for name, chCfg := range a.Cfg.Channels {
			capCfg := capability.ChannelConfig{
				Token:         chCfg.Token,
				SigningSecret: chCfg.SigningSecret,
				WebhookURL:    chCfg.WebhookURL,
				PhoneNumberID: chCfg.PhoneNumberID,
			}
			ch, err := a.CapReg.GetChannel(name, capCfg)
			if err != nil {
				a.Log.Warn("channel not available", "name", name, "error", err)
				continue
			}
			a.ChannelOrch.Register(name, ch)
		}
		a.ChannelOrch.StartAll(context.Background())
	}

	// ── Model router ─────────────────────────────────────────────────
	a.ModelRouter = routing.NewRouter(a.Cfg.Agent.DefaultModel)
	for _, r := range routing.DefaultRoutes() {
		a.ModelRouter.SetRoute(r.Kind, a.Cfg.Agent.DefaultModel)
	}
	var providerModels []routing.ProviderModel
	for _, p := range a.Cfg.Providers {
		providerModels = append(providerModels, routing.ProviderModel{Name: p.Name, Models: p.Models})
	}
	if len(providerModels) > 0 {
		a.ModelRouter.ConfigureFromProviders(providerModels)
	}

	// ── Cron ─────────────────────────────────────────────────────────
	a.Cron = cron.New(a.Log)
	if a.DB != nil {
		a.Cron.WithStore(a.DB)
	}
	if a.ChatService != nil {
		a.Cron.WithChatSender(NewCronChatSender(a.ChatService))
	}
	a.Cron.WithShellRunner(NewShellRunner())
	// Cron jobs are registered by the "cron-jobs" event bundle; the scheduler
	// is started after all bundles have run.

	// ── Subconscious ─────────────────────────────────────────────────
	a.Subcon = subconscious.NewPersistent(a.Log, a.Cfg.Workspace)
	// Evaluators are registered by the "subconscious" event bundle.

	// ── Heartbeat ────────────────────────────────────────────────────
	a.Heartbeat = heartbeat.New(a.Log, heartbeatInterval(a.Cfg))
	// The heartbeat handler is registered and started by the "heartbeat" event
	// bundle after all dependencies exist.

	// ── Learning ─────────────────────────────────────────────────────
	a.ToolTracker = learning.NewToolTrackerHook(nil)
	if a.Cfg.Learning.Enabled {
		a.Learning = learning.New(a.Log)
		if a.Provider != nil {
			a.Learning.SetProvider(a.Provider, a.Cfg.Agent.DefaultModel)
		}
		if a.DB != nil {
			store, _ := learning.NewSQLiteStore(a.DB)
			a.Learning.UseSQLiteStore(store)
			if cache, err := learning.NewSQLiteCache(a.DB); err == nil {
				a.Learning.UseFacetSystem(cache)
				a.Learning.StartFacetRebuildLoop(context.Background())
				a.Log.Info("learning facet system activated")
			}
		}
	} else {
		a.Log.Info("learning disabled by config")
	}

	// ── Metrics ──────────────────────────────────────────────────────
	a.Metrics = health.NewRegistry()

	// ── Cost tracker ─────────────────────────────────────────────────
	// Records per-turn token usage so the cost dashboard RPC has real data.
	a.CostTracker = cost.NewTracker(a.Cfg.Cost.BudgetCents)

	// ── Monitor manager ──────────────────────────────────────────────
	a.MonitorMgr = monitor.NewManager(a.Log)
	if a.DB != nil {
		a.MonitorMgr.WithDB(a.DB)
		a.MonitorMgr.RestoreFromDB()
	}

	// ── Sync manager ─────────────────────────────────────────────────
	a.SyncMgr = memsync.NewManager(a.Log)
	memsync.RegisterEnvConnectors(a.SyncMgr, a.Log)
	// The sync cron job is registered by the "cron-jobs" event bundle.

	// ── Event bundles ────────────────────────────────────────────────
	// Register post-turn hooks, cron jobs, subconscious evaluators, and the
	// heartbeat handler now that every dependency (Metrics, ToolTracker,
	// CostTracker, Learning, SyncMgr, NotifBus, Subcon, Heartbeat, Pipeline)
	// has been created. This ordering also fixes the pre-existing bug where the
	// metrics/tool-tracker/cost hooks were registered before their components
	// existed and therefore never ran.
	eventDeps := &bundle.Deps{
		Reg:          a.CapReg,
		Cfg:          a.Cfg,
		Workspace:    a.Cfg.Workspace,
		SecurityTier: a.Cfg.Security.Tier,
		Log:          a.Log,
		ConvStore:    a.ConvStore,
		Metrics:      a.Metrics,
		ToolTracker:  a.ToolTracker,
		CostTracker:  a.CostTracker,
		HookReg:      a.HookReg,
		Cron:         a.Cron,
		Pipeline:     a.Pipeline,
		Provider:     a.Provider,
		Learning:     a.Learning,
		SyncMgr:      a.SyncMgr,
		Subcon:       a.Subcon,
		Heartbeat:    a.Heartbeat,
		NotifBus:     a.NotifBus,
	}
	eventRegistry := bundle.NewRegistry(a.Cfg.Bundles.Disabled)
	if _, err := eventRegistry.Run(context.Background(), eventDeps, bundle.EventBundles()); err != nil {
		a.Log.Error("event bundle registration failed", "error", err)
	}

	// Start the schedulers after their jobs/handlers are registered.
	if a.Cron != nil {
		a.Cron.Start()
	}
	if a.Heartbeat != nil {
		a.Heartbeat.Start()
	}

	// ── Artifact store ───────────────────────────────────────────────
	if a.DB != nil {
		_ = artifacts.NewStore(a.Cfg.Workspace) // artifact store created for future use
	}

	// ── Post-bootstrap tools ─────────────────────────────────────────
	// GUI calls RegisterPostBootstrapTools again with the real automator.

	// ── Integrations ─────────────────────────────────────────────────
	intReg := integration.NewRegistry(a.Log)
	WireIntegrations(a.CapReg, intReg)

	// ── Wire extension RPC ───────────────────────────────────────────
	if a.CapReg != nil {
		capability.WireRPC(a.CapReg)
	}

	// ── eino integration ──────────────────────────────────────────────
	// Create the eino agent pipeline and wire it to ChatService.
	// ChatService uses the eino Runner exclusively (no legacy Loop).
	chatModel, err := eino.NewChatModel(context.Background(), a.Cfg)
	if err != nil {
		a.Log.Warn("eino: failed to create chat model, skipping integration", "err", err)
	}

	if chatModel != nil {
		// Collect failover models from alternative providers.
		primaryPC := a.Cfg.FindProviderForModel(a.Cfg.Agent.DefaultModel)
		failoverModels, _ := eino.CollectFailoverModels(context.Background(), a.Cfg, primaryPC)
		if len(failoverModels) > 0 {
			a.Log.Info("eino: failover models available",
				"count", len(failoverModels))
		}
		// The CapabilityRegistry is the single source of truth for tools:
		// builtin bundles, extensions, MCP, memory, and config tools are all
		// registered there. The agent set adapts every registry tool directly,
		// so [bundles] disabled is honored and there is no second hardcoded
		// tool list to drift out of sync.
		allTools := capability.CollectRegistryTools(a.CapReg, nil)
		if len(allTools) > 0 {
			a.Log.Info("eino: using registry tools", "count", len(allTools))
		}

		secMW := &einomw.SecurityMiddleware{
			Policy:       a.SecurityPolicy,
			ApprovalGate: a.ApprovalGate,
			AuditLogger:  a.AuditLogger,
		}

		breakerMW := einomw.NewCircuitBreakerWithConfig(
			a.Cfg.CircuitBreaker.MaxRepeatFailures,
			a.Cfg.CircuitBreaker.MaxNoProgressFails,
			0,
		)

		memMW := &einomw.MemoryMiddleware{
			Pipeline:   a.Pipeline,
			Prefetcher: a.MemoryPrefetcher,
			Profile:    a.ProfileStore,
			Tracker:    a.SessionTracker,
			Log:        a.Log,
			Skills:     a.SkillsInjector,
			Exp:        a.ExperienceInjector,
			Workflows:  a.WorkflowInjector,
		}

		cbm := einocb.NewManager(a.AuditLogger, nil, a.Learning)
		a.Callbacks = cbm

		agentSet, agentSetErr := eino.NewAgentSet(context.Background(), &eino.AgentSetConfig{
			Workspace:       a.Cfg.Workspace,
			PromptMgr:       a.PromptMgr,
			FailoverModels:  failoverModels,
			ChatModel:       chatModel,
			AllTools:        allTools,
			MessageModifier: memMW.ModifyMessages,
			SecurityMW:      secMW,
			BreakerMW:       breakerMW,
		}, &agentRegistryAdapter{capReg: a.CapReg})
		if agentSetErr != nil {
			a.Log.Warn("eino: failed to create agent set", "err", agentSetErr)
		}

		if agentSet != nil {
			a.Runner, err = eino.NewRunner(context.Background(), eino.RunnerConfig{
				AgentSet:        agentSet,
				Callbacks:       cbm,
				MemoryMW:        memMW,
				SecurityMW:      secMW,
				BreakerMW:       breakerMW,
				CheckPointStore: a.CheckPointStore,
				Log:             a.Log,
			})
			if err != nil {
				a.Log.Warn("eino: failed to create runner", "err", err)
			} else if a.ChatService != nil {
				a.ChatService.SetRunner(a.Runner)
				a.ChatService.SetAuditLogger(a.AuditLogger)
				a.Log.Info("eino: runner wired to ChatService bridge")
			}
		}
	}

	// ── Background task execution ────────────────────────────
	// DeepCopy empty []string to avoid issues with time.Now() in sed
	if a.Runner != nil {
		a.BackgroundRunner = agent.NewBackgroundRunner(a.Runner, a.Log)
		a.Log.Info("background runner initialized")

		// Wire DAG Runner with agent integration.
		if a.DB != nil {
			dagStore, err := dag.NewStore(a.DB)
			if err != nil {
				a.Log.Warn("dag store init failed", "error", err)
			} else {
				a.DAGStore = dagStore
				dagExecutor := &dag.NodeExecutor{
					AgentRunner: func(ctx context.Context, prompt string) (string, error) {
						result := a.BackgroundRunner.RunAsync(ctx, "dag_agent", prompt)
						// Block until complete for deterministic DAG execution.
						for evt := range result {
							if evt.Status == "completed" && evt.Result != nil {
								return evt.Result.Response, nil
							}
							if evt.Status == "failed" {
								return "", fmt.Errorf("agent node failed: %s", evt.Error)
							}
						}
						return "", fmt.Errorf("agent node completed without result")
					},
				}
				a.DAGRunner = dag.NewRunner(dag.RunnerConfig{
					Executor: dagExecutor,
					Store:    dagStore,
					Log:      a.Log,
				})
				// Re-register tools with the wired runner, only when the DAG
				// bundle is enabled (the bootstrap-time placeholder was also
				// gated, so this must be too or the disable is ineffective).
				if bundle.NewRegistry(a.Cfg.Bundles.Disabled).IsEnabled(bundle.BundleDAG) {
					dag.RegisterTools(a.CapReg, a.DAGRunner, dagStore)
					a.Log.Info("dag runner initialized")
				}
			}
		}
	}
	if a.DB != nil {
		taskStore, err := agent.NewTaskStore(a.DB)
		if err != nil {
			a.addStartupError("task store", err)
		} else if taskStore != nil && a.BackgroundRunner != nil {
			a.TaskStore = taskStore
			executor := func(ctx context.Context, task *agent.DispatchTask) error {
				ch := a.BackgroundRunner.RunAsync(ctx, task.ID, task.Prompt)
				for evt := range ch {
					if a.EventBus != nil {
						a.EventBus.PublishTyped(events.DomainAgent, "task.progress", evt)
					}
					if evt.Status == "completed" && evt.Result != nil {
						task.Result = evt.Result.Response
						return nil
					}
					if evt.Status == "failed" {
						return fmt.Errorf("%s", evt.Error)
					}
					if evt.Status == "interrupted" {
						return fmt.Errorf("task interrupted: checkpoint saved at %s", evt.CheckPointID)
					}
				}
				return nil
			}
			a.TaskDispatcher = agent.NewTaskDispatcher(executor, nil)
			a.TaskDispatcher.WithStore(taskStore)
			a.TaskDispatcher.Start(context.Background())
			a.Log.Info("task dispatcher initialized with persistent store")
		}
	}
	a.Log.Info("app core initialization complete")
}

// Shutdown stops all components in reverse dependency order.
func (a *AppCore) Shutdown() {
	a.Log.Info("app core stopping")
	// Shutdown event published by caller before unsubscribing.
	if a.CapReg != nil {
		a.CapReg.Shutdown()
	}
	if a.ChannelOrch != nil {
		a.ChannelOrch.StopAll()
	}
	if a.Heartbeat != nil {
		a.Heartbeat.Stop()
	}
	if a.Cron != nil {
		a.Cron.Stop()
	}
	if a.Subcon != nil {
		a.Subcon.Close()
	}
	if a.Pipeline != nil {
		a.Pipeline.Stop()
	}
	if a.Learning != nil {
		a.Learning.Shutdown()
	}
	if a.DB != nil {
		a.DB.Close()
	}
}

// Health returns a health snapshot.
func (a *AppCore) Health() map[string]interface{} {
	status := map[string]interface{}{"status": "ok"}
	if len(a.StartupErrors) > 0 {
		status["status"] = "degraded"
		status["startup_errors"] = a.StartupErrors
	}
	if a.CapReg != nil {
		status["tools"] = a.CapReg.ToolNames()
	}
	for k, v := range ProviderDiagnostics(a.Cfg, a.Provider, nil) {
		status[k] = v
	}
	return status
}

func (a *AppCore) addStartupError(subsystem string, err error) {
	msg := subsystem + ": " + err.Error()
	a.Log.Error("startup error", "subsystem", subsystem, "error", err)
	a.StartupErrors = append(a.StartupErrors, msg)
	if a.EventBus != nil {
		a.EventBus.PublishTyped(events.DomainSystem, events.KindSystemStartup, map[string]interface{}{
			"error":     msg,
			"subsystem": subsystem,
		})
	}
}

// approvalGateEnabled checks MNEME_APPROVAL_GATE env var.
// Set to "0" or "false" to disable.
func approvalGateEnabled() bool {
	v := os.Getenv("MNEME_APPROVAL_GATE")
	if v == "" {
		return true
	}
	return v != "0" && v != "false"
}

// agentRegistryAdapter adapts capability.CapabilityRegistry to the
// eino.AgentRegistry interface, allowing NewAgentSet to dynamically
// create agents from the registry instead of hardcoding them.
type agentRegistryAdapter struct {
	capReg *capability.CapabilityRegistry
}

func (a *agentRegistryAdapter) AgentDefs() []eino.AgentDef {
	if a.capReg == nil {
		return nil
	}
	descs := a.capReg.AllAgents()
	defs := make([]eino.AgentDef, 0, len(descs))
	for _, d := range descs {
		systemPrompt := ""
		if def, ok := a.capReg.GetAgent(d.ID); ok && def != nil {
			systemPrompt = def.SystemPrompt
		}
		defs = append(defs, eino.AgentDef{
			ID:            d.ID,
			Name:          d.Name,
			Description:   d.Description,
			ToolAllowlist: d.ToolAllowlist,
			Hidden:        d.Hidden,
			SystemPrompt:  systemPrompt,
		})
	}
	return defs
}
