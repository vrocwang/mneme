package boot

import (
	"context"
	"path/filepath"
	"time"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/bundle"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/desktop"
	"github.com/simon/mneme/internal/jsonrpc"
	"github.com/simon/mneme/internal/voice"
	"github.com/simon/mneme/internal/webhooks"
	workspace "github.com/simon/mneme/internal/workspace"
)

// GUIComponents holds GUI-only components created during startup.
// These are created by BootstrapGUI and cleaned up by ShutdownGUI.
type GUIComponents struct {
	ScreenCap           *desktop.ScreenCapture
	CompanionLoop       *desktop.CompanionLoop
	ScreenIntel         *desktop.ScreenIntelLoop
	InferenceHTTPServer *jsonrpc.Server
	WebhookServer       *webhooks.Server
	WebhookTM           *webhooks.TunnelManager
	TriagePipeline      *agent.TriagePipeline
}

// BootstrapGUI creates and starts GUI-only components (desktop companion,
// inference HTTP server, webhook server). Returns the components for
// cleanup at shutdown.
//
// appMethods is an optional jsonrpc.AppMethods implementation for the
// inference HTTP server. Pass nil to skip method registration.
func BootstrapGUI(core *AppCore, ctx context.Context, appMethods jsonrpc.AppMethods) *GUIComponents {
	gui := &GUIComponents{}
	log := core.Log
	cfg := core.Cfg

	// ── Desktop components (disableable via [bundles] disabled = ["desktop"]) ──
	// Mouse/keyboard/accessibility/automation primitives live in the
	// desktop-auto extension and screen capture in screen-intel; the core keeps
	// only the companion loop and screen-intel loop, which need the in-process
	// provider.
	if bundle.NewRegistry(cfg.Bundles.Disabled).IsEnabled(bundle.BundleDesktop) {
		gui.ScreenCap = desktop.NewScreenCapture(workspace.ScreenshotsDir(cfg.Workspace))
		var visionEngine *desktop.VisionEngine
		if core.Provider != nil {
			visionEngine = desktop.NewVisionEngine(desktop.VisionConfig{
				Provider: core.Provider, Model: VisionModel(cfg), Logger: log,
			})
			gui.CompanionLoop = desktop.NewCompanionLoop(log, desktop.CompanionConfig{
				STT:      voice.BuildSTT(cfg),
				TTS:      voice.BuildTTS(cfg),
				Screen:   gui.ScreenCap,
				Provider: core.Provider,
				Model:    cfg.Agent.DefaultModel,
				Vision:   visionEngine,
				TempDir:  workspace.TmpDir(cfg.Workspace),
			})
			if cfg.ScreenIntelligence.Enabled && gui.ScreenCap != nil && visionEngine != nil {
				// Persist, ingest, and publish each observation. Without this
				// sink the loop computes captures and vision summaries and then
				// discards them.
				sink := NewScreenIntelSink(log, workspace.DataDir(cfg.Workspace),
					screenIntelIngester(core.Pipeline), core.EventBus)
				gui.ScreenIntel = desktop.NewScreenIntelLoop(log, desktop.ScreenIntelConfig{
					Screen:   gui.ScreenCap,
					Vision:   visionEngine,
					Interval: time.Duration(cfg.ScreenIntelligence.CaptureIntervalSecs) * time.Second,
					OnResult: sink.Handle,
				})
				gui.ScreenIntel.Start(ctx)
			}
		}
	} else {
		log.Info("desktop bundle disabled by config")
	}

	// ── Inference HTTP server ─────────────────────────────────────────
	gui.InferenceHTTPServer = jsonrpc.Bootstrap(cfg, core.Provider, core.EventBus, core.AuditLogger, log)
	if gui.InferenceHTTPServer != nil {
		if appMethods != nil {
			jsonrpc.RegisterAppMethods(gui.InferenceHTTPServer, appMethods)
		}
		go func() {
			if err := gui.InferenceHTTPServer.Start(); err != nil {
				log.Error("inference HTTP server failed", "error", err)
			}
		}()
	}

	// ── Webhook server ────────────────────────────────────────────────
	// Use the eino graph-based triage pipeline when a provider is
	// available, falling back to the rules-only pipeline.
	var triagePipeline *agent.TriagePipeline
	if core.Provider != nil {
		p, err := agent.NewGraphTriagePipeline(core.CapReg, core.ChatService,
			core.Provider, cfg.ModelForRoute(config.RouteReasoning))
		if err != nil {
			log.Warn("graph triage pipeline failed, falling back to rules-only", "error", err)
			triagePipeline = agent.NewDefaultTriagePipeline(core.CapReg, core.ChatService)
		} else {
			triagePipeline = p
		}
	} else {
		triagePipeline = agent.NewDefaultTriagePipeline(core.CapReg, core.ChatService)
	}
	gui.TriagePipeline = triagePipeline
	gui.WebhookTM = webhooks.NewTunnelManager(filepath.Join(workspace.DataDir(cfg.Workspace), "tunnels.json"))
	gui.WebhookServer = webhooks.Bootstrap(cfg, gui.WebhookTM, triagePipeline, core.EventBus, log)
	if gui.WebhookServer != nil {
		go func() {
			if err := gui.WebhookServer.Start(); err != nil {
				log.Error("webhook server failed", "error", err)
			}
		}()
	}

	// Cron and monitor tools are registered by AppCore.Init so they are
	// available in headless modes too; nothing to do here.

	log.Info("app startup complete")
	return gui
}

// ShutdownGUI stops all GUI-only components in reverse order.
func ShutdownGUI(gui *GUIComponents) {
	if gui == nil {
		return
	}
	if gui.InferenceHTTPServer != nil {
		gui.InferenceHTTPServer.Stop()
	}
	if gui.WebhookServer != nil {
		gui.WebhookServer.Stop()
	}
	if gui.TriagePipeline != nil {
		gui.TriagePipeline.Stop()
	}
	if gui.CompanionLoop != nil {
		gui.CompanionLoop.Stop()
	}
	if gui.ScreenIntel != nil {
		gui.ScreenIntel.Stop()
	}
}
