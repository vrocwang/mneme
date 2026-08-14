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
	return core, nil
}
