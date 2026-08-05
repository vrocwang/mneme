//go:build darwin

package cwd_jail

import (
	"context"
	"fmt"
	"os/exec"
)

// seatbeltBackend enforces jail restrictions using macOS Seatbelt (sandbox-exec).
// Not yet implemented — full Seatbelt integration with sandbox profiles requires
// profile generation matching the Rust seatbelt backend.
type seatbeltBackend struct{}

func NewSeatbelt() Backend {
	return &seatbeltBackend{}
}

func (s *seatbeltBackend) Name() string { return "seatbelt" }

func (s *seatbeltBackend) IsAvailable() bool {
	// sandbox-exec exists on macOS but without profile generation we cannot
	// enforce restrictions. Return false so callers fall back to noop and
	// get a clear warning rather than a false sense of security.
	return false
}

func (s *seatbeltBackend) Spawn(ctx context.Context, jail *Jail, cmd string, args ...string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("seatbelt: sandbox enforcement is not yet implemented on macOS — commands cannot be jailed")
}
