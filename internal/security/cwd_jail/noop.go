package cwd_jail

import (
	"context"
	"log/slog"
	"os/exec"
)

// noopBackend runs commands without any sandboxing. Used as a fallback
// when no platform-specific backend is available.
type noopBackend struct{}

func (n *noopBackend) Name() string      { return "noop" }
func (n *noopBackend) IsAvailable() bool { return true }

func (n *noopBackend) Spawn(ctx context.Context, jail *Jail, cmd string, args ...string) (*exec.Cmd, error) {
	slog.Warn("cwd_jail: no sandbox backend available — command will run unrestricted",
		"backend", "noop",
		"jail", jail.Label,
		"cmd", cmd,
	)
	all := append([]string{cmd}, args...)
	c := exec.CommandContext(ctx, all[0], all[1:]...)
	c.Dir = jail.Root
	return c, nil
}
