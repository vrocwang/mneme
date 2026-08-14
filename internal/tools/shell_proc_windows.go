//go:build windows

package tools

import "os/exec"

// setProcessGroup is a no-op on Windows; process trees are terminated via
// Job Objects elsewhere. killProcessTree falls back to killing the direct
// process, matching the previous behavior.
func setProcessGroup(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
