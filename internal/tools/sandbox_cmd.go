package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security/sandbox"
)

// sandboxBackend is the sandbox backend, defaulting to auto-detection at init.
// Use SetSandboxConfig to override before tool execution begins.
var sandboxBackend = sandbox.Detect()

// sandboxRequired is true when the operator explicitly requested sandboxing
// (BackendOverride, or Mode == "sandboxed") but no real backend is available on
// this platform. When set, sandboxCmd fails closed instead of silently running
// commands unrestricted.
var sandboxRequired bool

// SetSandboxConfig overrides the sandbox backend used by sandboxCmd.
// Call during boot, before any tools execute, to apply config-driven
// sandbox selection. If an explicit sandbox backend is requested but resolves
// to a noop (unsupported platform), sandboxing is marked required so tool
// execution fails closed rather than silently degrading.
func SetSandboxConfig(cfg config.SandboxConfig) {
	switch {
	case cfg.Mode == "disabled":
		sandboxBackend = sandbox.NoopBackend()
		sandboxRequired = false
	case cfg.BackendOverride != "":
		sandboxBackend = sandbox.NewByName(cfg.BackendOverride)
		sandboxRequired = sandbox.IsNoop(sandboxBackend)
		if sandboxRequired {
			slog.Error("sandbox: requested backend unavailable; tool execution will be refused",
				"backend_override", cfg.BackendOverride)
		}
	default:
		sandboxBackend = sandbox.Detect()
		sandboxRequired = cfg.Mode == "sandboxed" && sandbox.IsNoop(sandboxBackend)
		if sandboxRequired {
			slog.Error("sandbox: mode=sandboxed requested but no sandbox backend is available; tool execution will be refused")
		}
	}
}

// sandboxCmd creates an *exec.Cmd that runs inside the sandbox when available.
// It fails closed (returns an error) when sandboxing was explicitly required but
// is unavailable, rather than silently running unrestricted.
// workspaceRoot is the writable directory exposed to the sandboxed command.
func sandboxCmd(ctx context.Context, workspaceRoot, command string, args ...string) (*exec.Cmd, error) {
	if sandboxRequired {
		return nil, fmt.Errorf("sandbox is required by configuration but no sandbox backend is available on this platform")
	}
	if sandboxBackend.Available() {
		cmd, err := sandboxBackend.WrapCommand(ctx, workspaceRoot, command, args...)
		if err == nil {
			return cmd, nil
		}
	}
	// No real sandbox available and none was explicitly required: fall back to
	// a plain command (the noop backend already logs a warning).
	return exec.CommandContext(ctx, command, args...), nil
}
