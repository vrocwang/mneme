//go:build !windows

package subprocess

import (
	"os/exec"
	"syscall"
)

// setProcessGroup places the command in its own process group so that
// killProcessTree can terminate the command and all of its children.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills the command's entire process group (negative PID
// targets the group), so children spawned by `sh -c` are also terminated.
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
