//go:build !windows

package tools

import "os/exec"

func hideConsoleWindow(cmd *exec.Cmd) {}
