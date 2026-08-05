package agent

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// InterruptFence provides graceful Ctrl+C handling with double-tap force-exit.
// Single interrupt → graceful stop channel is closed.
// Double interrupt within 2s → process exits immediately.
type InterruptFence struct {
	mu      sync.Mutex
	stopCh  chan struct{}
	doneCh  chan struct{}
	sigCh   chan os.Signal
	lastHit time.Time
	stopped bool
}

// NewInterruptFence creates an interrupt fence and starts listening for OS signals.
// Call Stop() to clean up.
func NewInterruptFence() *InterruptFence {
	f := &InterruptFence{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	f.sigCh = sigCh

	go func() {
		defer close(f.doneCh)
		for sig := range sigCh {
			f.mu.Lock()

			now := time.Now()
			if now.Sub(f.lastHit) < 2*time.Second {
				// Double-tap: force exit.
				f.mu.Unlock()
				os.Exit(1)
			}
			if f.stopped {
				f.mu.Unlock()
				return
			}
			f.lastHit = now

			// First tap: graceful stop.
			close(f.stopCh)
			f.stopped = true
			f.mu.Unlock()
			_ = sig
		}
	}()

	return f
}

// Stop cleans up the signal handler and closes the fence.
func (f *InterruptFence) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stopped {
		f.stopped = true
		select {
		case <-f.stopCh:
			// already closed
		default:
			close(f.stopCh)
		}
	}
	signal.Stop(f.sigCh) // stop signals on the original channel
	close(f.sigCh)       // unblock the goroutine ranging over sigCh
}

// InterruptChannel returns a channel that is closed when an interrupt is received.
// The agent loop should select on this channel between iterations.
func (f *InterruptFence) InterruptChannel() <-chan struct{} {
	return f.stopCh
}

// Done returns a channel that is closed when the signal handler goroutine exits.
func (f *InterruptFence) Done() <-chan struct{} {
	return f.doneCh
}
