//go:build !windows && !linux

package monitor

import (
	"io"
	"os/exec"

	"github.com/creack/pty"
)

// ptyProc holds the result of starting a command with a pseudo-terminal.
type ptyProc struct {
	out     io.ReadCloser // combined stdout/stderr
	cleanup func()        // releases PTY resources
	wait    func() error  // waits for command to exit
	kill    func()        // forcefully terminates the command
}

// startPTY starts cmd with a pseudo-terminal via creack/pty (macOS, BSD, etc.).
func startPTY(cmd *exec.Cmd) (*ptyProc, error) {
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &ptyProc{
		out:     f,
		cleanup: func() { f.Close() },
		wait:    cmd.Wait,
		kill:    func() { cmd.Process.Kill() },
	}, nil
}
