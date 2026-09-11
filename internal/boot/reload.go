package boot

import (
	"context"
	"log/slog"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	eino "github.com/simon/mneme/internal/eino"
	einocb "github.com/simon/mneme/internal/eino/callbacks"
	einomw "github.com/simon/mneme/internal/eino/middleware"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/learning"
	"github.com/simon/mneme/internal/security"
	"github.com/simon/mneme/internal/tools"
)

// ReloadConfig recreates the inference provider from the current config so that
// provider/model changes in the Settings UI take effect without restarting the
// application. The eino pipeline is rebuilt separately by ReloadEino.
func ReloadConfig(
	cfg *config.Config,
	learn *learning.Engine,
	log *slog.Logger,
	setProvider func(inference.Provider),
) {
	newProvider := NewProvider(cfg)
	if newProvider == nil {
		log.Warn("config reload: no provider for configured model")
		return
	}
	setProvider(newProvider)
	sc := cfg.Security.Commands
	security.SetExtraAllowedCommands(sc.ExtraAllowedCommands)
	security.SetCommandPolicy(sc.BlockHighRisk, sc.RequireMediumApproval)
	security.SetCommandOverrides(sc.ExtraReadOnly, sc.ExtraNetwork, sc.ExtraDestructive, sc.ExtraInstall, sc.ExtraExecutors, sc.ExtraDangerousEnv)
	log.Info("config reload: provider updated", "name", newProvider.Name())

	if learn != nil {
		learn.SetProvider(newProvider, cfg.ModelForRoute(config.RouteReasoning))
	}
}

// ReloadEino rebuilds the eino ChatModel, AgentSet, and Runner from the
// current config so that provider changes (e.g. switching from Ollama to
// DeepSeek) take effect without restarting the application.
func ReloadEino(a *AppCore) {
	if a == nil || a.Cfg == nil || a.ChatService == nil {
		return
	}

	// Rebuild the security policy from the current config and re-apply the
	// tool-level settings, so security tier, forbidden paths, sandbox mode and
	// the default tool timeout take effect without an app restart.
	a.SecurityPolicy = a.Cfg.BuildSecurityPolicy()
	tools.SetToolTimeoutSecs(a.Cfg.Agent.Limits.DefaultToolTimeout)
	tools.SetSandboxConfig(a.Cfg.Sandbox)

	chatModel, err := eino.NewChatModel(context.Background(), a.Cfg)
	if err != nil {
		a.Log.Warn("config reload: failed to create eino chat model", "err", err)
		return
	}

	primaryPC := a.Cfg.FindProviderForModel(a.Cfg.Agent.DefaultModel)
	failoverModels, _ := eino.CollectFailoverModels(context.Background(), a.Cfg, primaryPC)

	// The CapabilityRegistry is the single source of truth for tools; the
	// agent set adapts every registry tool directly so [bundles] disabled is
	// honored on reload too.
	allTools := capability.CollectRegistryTools(a.CapReg, nil)

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
	cbm := einocb.NewManager(a.AuditLogger, a.Learning)
	a.Callbacks = cbm

	routeModels := eino.CollectRouteModels(context.Background(), a.Cfg, a.Log)
	agentSet, agentSetErr := eino.NewAgentSet(context.Background(), &eino.AgentSetConfig{
		Workspace:            a.Cfg.Workspace,
		PromptMgr:            a.PromptMgr,
		FailoverModels:       failoverModels,
		ChatModel:            chatModel,
		AllTools:             allTools,
		MessageModifier:      memMW.ModifyMessages,
		SecurityMW:           secMW,
		BreakerMW:            breakerMW,
		DefaultMaxIterations: a.Cfg.Agent.Limits.MaxToolRounds,
		Models:               routeModels,
	}, &agentRegistryAdapter{capReg: a.CapReg})
	if agentSetErr != nil {
		a.Log.Warn("config reload: failed to create agent set", "err", agentSetErr)
		return
	}

	runner, err := eino.NewRunner(context.Background(), eino.RunnerConfig{
		AgentSet:        agentSet,
		Callbacks:       cbm,
		MemoryMW:        memMW,
		SecurityMW:      secMW,
		BreakerMW:       breakerMW,
		CheckPointStore: a.CheckPointStore,
		Log:             a.Log,
	})
	if err != nil {
		a.Log.Warn("config reload: failed to create runner", "err", err)
		return
	}

	a.Runner = runner
	a.ChatService.SetRunner(runner)
	a.Log.Info("config reload: eino runner rebuilt", "model", a.Cfg.Agent.DefaultModel)
}
