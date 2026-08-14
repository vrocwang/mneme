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
	"github.com/simon/mneme/internal/routing"
	"github.com/simon/mneme/internal/security"
)

// ReloadConfig recreates the inference provider, model router, and eino Runner
// from the current config so that provider/model changes in the Settings UI
// take effect without restarting the application.
func ReloadConfig(
	cfg *config.Config,
	learn *learning.Engine,
	log *slog.Logger,
	setProvider func(inference.Provider),
	setRouter func(*routing.Router),
) {
	newProvider := NewProvider(cfg)
	if newProvider == nil {
		log.Warn("config reload: no provider for configured model")
		return
	}
	setProvider(newProvider)
	security.SetExtraAllowedCommands(cfg.Security.Commands.ExtraAllowedCommands)
	log.Info("config reload: provider updated", "name", newProvider.Name())

	if learn != nil {
		learn.SetProvider(newProvider, cfg.Agent.DefaultModel)
	}

	// Rebuild model router from current config routes and providers.
	router := routing.NewRouter(cfg.Agent.DefaultModel)
	for _, r := range routing.DefaultRoutes() {
		router.SetRoute(r.Kind, cfg.Agent.DefaultModel)
	}
	var providerModels []routing.ProviderModel
	for _, p := range cfg.Providers {
		providerModels = append(providerModels, routing.ProviderModel{
			Name: p.Name, Models: p.Models,
		})
	}
	if len(providerModels) > 0 {
		router.ConfigureFromProviders(providerModels)
	}
	setRouter(router)
	log.Info("config reload: model router updated", "default", cfg.Agent.DefaultModel)
}

// ReloadEino rebuilds the eino ChatModel, AgentSet, and Runner from the
// current config so that provider changes (e.g. switching from Ollama to
// DeepSeek) take effect without restarting the application.
func ReloadEino(a *AppCore) {
	if a == nil || a.Cfg == nil || a.ChatService == nil {
		return
	}

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
	breakerMW := einomw.NewCircuitBreaker()
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
