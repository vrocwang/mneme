package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/simon/mneme/internal/boot"
	"github.com/simon/mneme/internal/jsonrpc"
	"github.com/simon/mneme/pkg/events"
)

func runServer(args []string) error {
	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	bus := events.NewBus(256)

	srv := jsonrpc.NewServer(log, core.Cfg.InferenceHTTP.Bind, core.Cfg.InferenceHTTP.Port, core.Provider, bus)
	if srv == nil {
		return fmt.Errorf("failed to create JSON-RPC server")
	}

	// Register health + agent endpoints.
	jsonrpc.RegisterAppMethods(srv, &cliAppMethods{core: core})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Info("shutting down...")
		srv.Stop()
	}()

	fmt.Fprintf(os.Stderr, "Mneme JSON-RPC server starting on %s:%d\n", core.Cfg.InferenceHTTP.Bind, core.Cfg.InferenceHTTP.Port)
	fmt.Fprintf(os.Stderr, "Endpoints:\n")
	fmt.Fprintf(os.Stderr, "  /health             — health check\n")
	fmt.Fprintf(os.Stderr, "  /api/rpc            — JSON-RPC 2.0\n")
	fmt.Fprintf(os.Stderr, "  /v1/chat/completions — OpenAI-compatible chat API\n")
	fmt.Fprintf(os.Stderr, "  /v1/models           — model list\n")

	return srv.Start()
}

// cliAppMethods satisfies jsonrpc.AppMethods for CLI health checks.
type cliAppMethods struct {
	core *boot.AppCore
}

func (a *cliAppMethods) Health() map[string]interface{} {
	status := map[string]interface{}{"status": "ok", "mode": "cli"}
	if a.core.CapReg != nil {
		status["tools"] = len(a.core.CapReg.ToolNames())
		status["agents"] = len(a.core.CapReg.AllAgents())
	}
	return status
}

func (a *cliAppMethods) ListAgents() []map[string]interface{} {
	if a.core.CapReg == nil {
		return nil
	}
	descs := a.core.CapReg.AllAgents()
	out := make([]map[string]interface{}, 0, len(descs))
	for _, d := range descs {
		if d.Hidden {
			continue
		}
		out = append(out, map[string]interface{}{
			"id": d.ID, "name": d.Name, "description": d.Description,
		})
	}
	return out
}

func (a *cliAppMethods) SearchMemory(query string) (string, error) {
	return "Memory search not available in CLI mode.", nil
}
