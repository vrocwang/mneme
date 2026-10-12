package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security/sandbox"
)

// Sandbox is the sandbox seam's consumer-side handle. It binds a sandbox
// backend (Definition: sandbox.Backend) together with the fail-closed
// requirement state, so consumers express "run this command in the sandbox"
// without depending on a specific provider.
//
// The seam is assembled once at boot via SetSandboxConfig; consumers hold a
// *Sandbox reference and call Command. Swapping providers (noop -> landlock)
// changes only the assembly point, never the consumer.
type Sandbox struct {
	backend  sandbox.Backend
	required bool
}

// globalSandbox is the process-wide seam instance, populated at boot. It is a
// package-level variable (not a per-tool field) because sandbox selection is
// global configuration; consumers read it through sandboxCmd.
var globalSandbox = newSandbox(sandbox.Detect(), false)

func newSandbox(backend sandbox.Backend, required bool) *Sandbox {
	return &Sandbox{backend: backend, required: required}
}

// Command wraps the command in the sandbox, or returns a plain command when no
// sandbox is required and none is available. It fails closed (returns an error)
// when sandboxing was explicitly required but is unavailable, rather than
// silently running unrestricted. workspaceRoot is the writable directory
// exposed to the sandboxed command.
func (s *Sandbox) Command(ctx context.Context, workspaceRoot, command string, args ...string) (*exec.Cmd, error) {
	if s.required {
		return nil, fmt.Errorf("sandbox is required by configuration but no sandbox backend is available on this platform")
	}
	if s.backend.Available() {
		cmd, err := s.backend.WrapCommand(ctx, workspaceRoot, command, args...)
		if err == nil {
			return cmd, nil
		}
		// Sandboxing was not mandatory, so fall open rather than refusing to
		// run — but never silently: the operator must be able to see that this
		// command ran unsandboxed and why.
		slog.Warn("sandbox: backend failed to wrap command; running unsandboxed",
			"command", command, "error", err)
	}
	// No real sandbox available and none was explicitly required: fall back to
	// a plain command (the noop backend already logs a warning).
	return exec.CommandContext(ctx, command, args...), nil
}

// SetSandboxConfig assembles the sandbox seam from configuration. Call during
// boot, before any tools execute. If an explicit sandbox backend is requested
// but resolves to a noop (unsupported platform), sandboxing is marked required
// so tool execution fails closed rather than silently degrading.
func SetSandboxConfig(cfg config.SandboxConfig) {
	globalSandbox = resolveSandbox(cfg)
}

// resolveSandbox is the single source of truth for turning a SandboxConfig
// into a Sandbox seam (backend + fail-closed requirement). Both the global
// seam (SetSandboxConfig) and the shell tool use it, so the fail-closed logic
// cannot drift between them.
//
// Accepted modes are the documented set — none, read_only, read_write,
// sandboxed — plus "disabled" as a legacy alias for "none". Every backend
// exposes one workspace-writable grant (sandbox.Backend has no read-only
// variant), so read_only and read_write both mean "use the best available
// sandbox"; only sandboxed makes the sandbox mandatory (fail closed).
func resolveSandbox(cfg config.SandboxConfig) *Sandbox {
	if cfg.BackendOverride != "" {
		backend := sandbox.NewByName(cfg.BackendOverride)
		required := sandbox.IsNoop(backend)
		if required {
			slog.Error("sandbox: requested backend unavailable; tool execution will be refused",
				"backend_override", cfg.BackendOverride)
		}
		return newSandbox(backend, required)
	}

	switch cfg.Mode {
	case "none", "disabled":
		return newSandbox(sandbox.NoopBackend(), false)
	case "sandboxed":
		backend := sandbox.Detect()
		required := sandbox.IsNoop(backend)
		if required {
			slog.Error("sandbox: mode=sandboxed requested but no sandbox backend is available; tool execution will be refused")
		}
		return newSandbox(backend, required)
	case "", "read_only", "read_write":
		return newSandbox(sandbox.Detect(), false)
	default:
		slog.Warn("sandbox: unknown mode, falling back to the detected sandbox",
			"mode", cfg.Mode, "valid", "none, read_only, read_write, sandboxed")
		return newSandbox(sandbox.Detect(), false)
	}
}

// sandboxCmd creates an *exec.Cmd that runs inside the sandbox when available.
// It is the consumer entry point for tools that need sandboxed execution.
// workspaceRoot is the writable directory exposed to the sandboxed command.
func sandboxCmd(ctx context.Context, workspaceRoot, command string, args ...string) (*exec.Cmd, error) {
	return globalSandbox.Command(ctx, workspaceRoot, command, args...)
}
