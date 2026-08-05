//go:build !linux

package sandbox

import "os/exec"

// LandlockBackend stub for non-Linux platforms.
type LandlockBackend struct{}

// NewLandlock always returns nil on non-Linux.
func NewLandlock() *LandlockBackend { return nil }

func (l *LandlockBackend) Name() string    { return "landlock" }
func (l *LandlockBackend) Available() bool { return false }
func (l *LandlockBackend) WrapCommand(cmd *exec.Cmd, workspace string) (*exec.Cmd, error) {
	return cmd, nil
}
