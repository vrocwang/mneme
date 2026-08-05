//go:build windows

package cwd_jail

import (
	"context"
	"fmt"
	"os/exec"
)

// appContainerBackend enforces jail restrictions using Windows AppContainer
// isolation. Not yet implemented — full AppContainer integration requires
// generating AppContainer profiles matching the Rust app_container backend.
type appContainerBackend struct{}

func NewAppContainer() Backend {
	return &appContainerBackend{}
}

func (a *appContainerBackend) Name() string { return "appcontainer" }

func (a *appContainerBackend) IsAvailable() bool {
	// AppContainer is available on Windows 8+ but without profile generation
	// we cannot enforce restrictions. Return false so callers fall back to
	// noop and get a clear warning rather than a false sense of security.
	return false
}

func (a *appContainerBackend) Spawn(ctx context.Context, jail *Jail, cmd string, args ...string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("appcontainer: sandbox enforcement is not yet implemented on Windows — commands cannot be jailed")
}
