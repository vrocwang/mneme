package main

import (
	"embed"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/simon/mneme/internal/about"
	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/app_state"
	"github.com/simon/mneme/internal/approval"
	"github.com/simon/mneme/internal/boot"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/channels"
	"github.com/simon/mneme/internal/cli"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/cost"
	"github.com/simon/mneme/internal/cron"
	"github.com/simon/mneme/internal/desktop"
	"github.com/simon/mneme/internal/health"
	"github.com/simon/mneme/internal/keyring"
	"github.com/simon/mneme/internal/learning"
	"github.com/simon/mneme/internal/logger"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/monitor"
	"github.com/simon/mneme/internal/registry"
	"github.com/simon/mneme/internal/soul"
	"github.com/simon/mneme/internal/webhooks"
	ws "github.com/simon/mneme/internal/workspace"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:generate go run cmd/build-extensions/main.go

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:extensions-dist
var embeddedExtensions embed.FS

func init() {
	ws.ExtensionSources = embeddedExtensions
	ws.HasExtensions = true
}

func main() {
	log := logger.NewDefault()

	// CLI mode: bypass Wails GUI and dispatch subcommands.
	if len(os.Args) > 1 && os.Args[1] == "cli" {
		if err := cli.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Print workspace directory notice on startup.
	workspace := config.WorkspaceDir()
	log.Info("workspace directory", "path", workspace)
	if os.Getenv("MNEME_HOME") == "" {
		os.Setenv("MNEME_HOME", workspace)
		fmt.Printf("→ Using workspace: %s\n", workspace)
		fmt.Printf("  Set MNEME_HOME env var to customize (e.g. MNEME_HOME=/path/to/dir ./mneme-go)\n")
	} else {
		fmt.Printf("→ Using custom workspace (MNEME_HOME): %s\n", workspace)
	}

	configPath := config.ConfigPath(workspace)
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("config load failed", "error", err)
		fmt.Fprintf(os.Stderr, "Fatal: cannot load config from %s: %v\n", configPath, err)
		os.Exit(1)
	}

	app := NewApp(cfg, log)

	// Optional startup delay for graceful service staggering.
	if delay := os.Getenv("MNEME_STARTUP_DELAY_SECS"); delay != "" {
		if d, err := strconv.Atoi(delay); err == nil && d > 0 {
			log.Info("startup delay active", "seconds", d)
			time.Sleep(time.Duration(d) * time.Second)
		}
	}

	// Register health checks (closures capture app fields by reference, evaluated lazily).
	health.Register("database", func() health.CheckResult {
		db := app.GetDB()
		if db == nil {
			return health.CheckResult{Status: "error", Message: "no database connection"}
		}
		if err := db.Ping(); err != nil {
			return health.CheckResult{Status: "error", Message: err.Error()}
		}
		return health.CheckResult{Status: "ok", Message: "connected"}
	})
	health.Register("provider", func() health.CheckResult {
		p := app.GetProvider()
		if p == nil {
			return health.CheckResult{Status: "error", Message: "no inference provider configured"}
		}
		return health.CheckResult{Status: "ok", Message: p.Name()}
	})
	health.Register("tools", func() health.CheckResult {
		capReg := app.GetCapReg()
		if capReg == nil {
			return health.CheckResult{Status: "error", Message: "capability registry not initialized"}
		}
		count := len(capReg.ToolNames())
		if count == 0 {
			return health.CheckResult{Status: "warning", Message: "no tools registered"}
		}
		return health.CheckResult{Status: "ok", Message: fmt.Sprintf("%d tools registered", count)}
	})
	health.Register("agents", func() health.CheckResult {
		capReg := app.GetCapReg()
		if capReg == nil {
			return health.CheckResult{Status: "error", Message: "capability registry not initialized"}
		}
		count := len(capReg.AllAgents())
		if count == 0 {
			return health.CheckResult{Status: "warning", Message: "no agents defined"}
		}
		return health.CheckResult{Status: "ok", Message: fmt.Sprintf("%d agents defined", count)}
	})

	err = wails.Run(&options.App{
		Title:  "Mneme",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:         &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		EnableDefaultContextMenu: true,
		OnStartup:                app.startup,
		OnShutdown:               app.shutdown,
		Bind: append([]interface{}{
			app,
			func() *config.ConfigRPC {
				rpc := config.NewConfigRPC(cfg, configPath).WithLogger(log)
				rpc.OnConfigChange(func() { app.onConfigReload() })
				return rpc
			}(),
			about.NewRPC(),
			keyring.NewKeyringRPC(),
			func() *approval.ApprovalRPC { rpc := approval.NewApprovalRPC(nil); app.SetApprovalRPC(rpc); return rpc }(),
			func() *capability.CapabilityRPC {
				rpc := capability.NewCapabilityRPC(nil)
				app.SetCapRPC(rpc)
				return rpc
			}(),
			func() *agent.AgentRPC { rpc := agent.NewAgentRPC(nil, ""); app.SetAgentRPC(rpc); return rpc }(),
			cron.NewCronRPC(app.GetCronScheduler()).WithChatSender(boot.NewCronChatSender(app.GetChatService())),
			learning.NewLearningRPC(app.GetLearningEngine()),
			memory.NewMemoryRPC(app.GetMemoryPipeline(), memory.NewNamespaceManager(app.GetDB())),
			func() *desktop.DesktopRPC { return desktop.NewDesktopRPC(app) }(),
			conversations.NewThreadsRPC(app.GetThreadStore),
			health.NewHealthRPC(workspace, app.GetDB()),
			func() *app_state.AppStateRPC {
				rpc := app_state.NewAppStateRPC(nil, app.GetApprovalGate(), nil, app.GetDB())
				app.SetAppStateRPC(rpc)
				return rpc
			}(),
			monitor.NewRPC(app.GetMonitorManager()),
			webhooks.NewWebhookRPC(app.GetWebhookTM()),
			channels.NewChannelRPC(cfg, configPath),
			soul.NewRPC(workspace),
			func() *registry.RPC { rpc := registry.NewRPC(nil, nil, nil); app.SetRegistryRPC(rpc); return rpc }(),
			cost.NewCostRPC(app.GetCostTracker()),
		}, capability.CollectWailsRPCBindings()...),
	})
	if err != nil {
		log.Error("app failed", "error", err)
	}
}
