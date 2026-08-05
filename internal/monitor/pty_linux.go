//go:build linux

package monitor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// ptyProc holds the result of starting a command with a pseudo-terminal.
type ptyProc struct {
	out     io.ReadCloser // master PTY — read to get combined stdout/stderr
	cleanup func()        // releases PTY resources
	wait    func() error  // waits for command to exit
	kill    func()        // forcefully terminates the command
}

// startPTY starts cmd with a Linux PTY (devpts). On failure the caller
// should fall back to pipe mode.
func startPTY(cmd *exec.Cmd) (*ptyProc, error) {
	// Open PTY master.
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}

	// unlockpt — make the slave accessible.
	if err := unix.IoctlSetInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, fmt.Errorf("unlockpt: %w", err)
	}

	// ptsname — get the slave index.
	ptyNum, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("ptsname: %w", err)
	}

	slavePath := "/dev/pts/" + strconv.Itoa(ptyNum)
	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("open %s: %w", slavePath, err)
	}

	// Create new session with the slave as controlling terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    int(slave.Fd()),
	}
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave

	if err := cmd.Start(); err != nil {
		master.Close()
		slave.Close()
		return nil, fmt.Errorf("start: %w", err)
	}

	// Close slave in parent — the child holds its own copy.
	slave.Close()

	cleanup := func() {
		master.Close()
	}

	wait := func() error {
		return cmd.Wait()
	}

	kill := func() {
		cmd.Process.Kill()
	}

	return &ptyProc{
		out:     master,
		cleanup: cleanup,
		wait:    wait,
		kill:    kill,
	}, nil
}
