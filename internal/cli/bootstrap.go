package cli

import (
	"fmt"
	"log/slog"
	"os"

	_ "github.com/simon/mneme/internal/sqlite"

	"github.com/simon/mneme/internal/boot"
	"github.com/simon/mneme/internal/config"
)

// bootstrap initializes a full AppCore for CLI operations (no ChatService).
func bootstrap() (*boot.AppCore, error) {
	return bootstrapLevel(false)
}

// bootstrapChat initializes a full AppCore for chat commands.
func bootstrapChat() (*boot.AppCore, error) {
	return bootstrapLevel(true)
}

// activeCores tracks every AppCore created during this CLI process so Run can
// tear them all down on exit. AppCore.Shutdown is idempotent.
var activeCores []*boot.AppCore

// ShutdownAll stops every AppCore created by this process, newest first. It is
// invoked once from Run's defer, so CLI commands never leak the DB handle,
// extension subprocesses, or background goroutines.
func ShutdownAll() {
	for i := len(activeCores) - 1; i >= 0; i-- {
		activeCores[i].Shutdown()
	}
	activeCores = nil
}

func bootstrapLevel(full bool) (*boot.AppCore, error) {
	workspace := config.WorkspaceDir()
	configPath := config.ConfigPath(workspace)

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	core := boot.NewAppCore(cfg, log)
	// For CLI, skip desktop/channels/webhook (headless mode).
	core.Init(true)
	activeCores = append(activeCores, core)
	return core, nil
}
