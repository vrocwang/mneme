package tools

import (
	"context"
	"os/exec"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security/sandbox"
)

// sandboxBackend is the sandbox backend, defaulting to auto-detection at init.
// Use SetSandboxConfig to override before tool execution begins.
var sandboxBackend = sandbox.Detect()

// SetSandboxConfig overrides the sandbox backend used by sandboxCmd.
// Call during boot, before any tools execute, to apply config-driven
// sandbox selection.
func SetSandboxConfig(cfg config.SandboxConfig) {
	switch {
	case cfg.Mode == "disabled":
		sandboxBackend = sandbox.NoopBackend()
	case cfg.BackendOverride != "":
		sandboxBackend = sandbox.NewByName(cfg.BackendOverride)
	default:
		sandboxBackend = sandbox.Detect()
	}
}

// sandboxCmd creates an *exec.Cmd that runs inside the sandbox when available.
// Falls back to a plain exec.CommandContext on platforms without sandbox support.
// workspaceRoot is the writable directory exposed to the sandboxed command.
func sandboxCmd(ctx context.Context, workspaceRoot, command string, args ...string) *exec.Cmd {
	if sandboxBackend.Available() {
		cmd, err := sandboxBackend.WrapCommand(ctx, workspaceRoot, command, args...)
		if err == nil {
			return cmd
		}
	}
	return exec.CommandContext(ctx, command, args...)
}
