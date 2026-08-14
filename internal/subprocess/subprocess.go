// Package subprocess defines the subprocess seam: the capability interface
// for executing a prepared *exec.Cmd with timeout and process-group
// termination. It is one of Mneme's cordis-style seams; see
// docs/adr/0002-seam-specification.md.
//
// The Definition (Runner) is this package's interface. OS is the in-process
// Provider backed by the host OS. Consumers (the shell tool and, later, other
// command-executing tools) depend only on Runner; the provider is injected at
// assembly points.
package subprocess

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// ErrTimeout is returned by Runner.Run when the command exceeds its timeout.
// The command's process tree has already been terminated when this is returned.
var ErrTimeout = errors.New("subprocess: command timed out")

// ErrCanceled is returned by Runner.Run when the parent context is canceled
// before the command completes. The command's process tree has already been
// terminated when this is returned.
var ErrCanceled = errors.New("subprocess: command canceled")

// Runner executes a prepared *exec.Cmd with a timeout. The caller owns the
// command's configuration (path, args, dir, env); Run only adds execution
// control: it places the process in its own group so a timeout can kill the
// whole tree, runs it, and returns the combined output.
type Runner interface {
	Run(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) (output []byte, err error)
}

// OS is the in-process Provider backed by the host OS.
type OS struct{}

// Compile-time check.
var _ Runner = OS{}

func (OS) Run(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) ([]byte, error) {
	// Run in its own process group so a timeout kills the whole tree (children
	// spawned by `sh -c` included), not just the direct child.
	setProcessGroup(cmd)

	type result struct {
		output []byte
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		o, e := cmd.CombinedOutput()
		ch <- result{o, e}
	}()

	select {
	case r := <-ch:
		// The command finished. If the parent context was canceled, the
		// sandbox's exec.CommandContext has already killed the process; report
		// a uniform ErrCanceled instead of the transport-specific "signal:
		// killed" / "context canceled" so the error surface is deterministic.
		if ctx.Err() != nil {
			return nil, ErrCanceled
		}
		return r.output, r.err
	case <-time.After(timeout):
		killProcessTree(cmd)
		return nil, ErrTimeout
	}
}
