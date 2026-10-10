package channels

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// channelRun tracks one started channel: the context derived for it (so it can
// be cancelled in isolation), its cancel func, and the done channel closed by
// its consume goroutine when it exits.
type channelRun struct {
	name   string
	ch     Channel
	cancel context.CancelFunc
	done   chan struct{}
}

// Orchestrator manages the lifecycle of all channels and consumes their
// events, routing each message to the dispatcher for agent processing.
// Owned by the channels package so app.go stays a thin wiring layer.
//
// Lifecycle is addressable per channel: StartAll/StartChannel start channels
// (each with its own derived context), StopChannel stops exactly one without
// touching the others, and StopAll tears everything down. Start/Stop are
// idempotent; concurrent start/stop is serialized by the lifecycle mutex.
type Orchestrator struct {
	log        *slog.Logger
	channels   map[string]Channel
	dispatcher *Dispatcher
	mu         sync.Mutex
	runs       map[string]*channelRun
	wg         sync.WaitGroup
	rootCtx    context.Context    // set by StartAll, shared by its derived runs
	rootCancel context.CancelFunc // cancels the consume-loop context on StopAll
	// lifecycle serializes start/stop transitions. Holding it across a
	// channel's Start/Stop call means two concurrent operations cannot
	// interleave (and cannot both decide a channel is "not running").
	lifecycle sync.Mutex
}

// NewOrchestrator creates a channel orchestrator.
func NewOrchestrator(log *slog.Logger, dispatcher *Dispatcher) *Orchestrator {
	return &Orchestrator{
		log:        log,
		channels:   make(map[string]Channel),
		dispatcher: dispatcher,
		runs:       make(map[string]*channelRun),
	}
}

// Register adds a channel without starting it.
func (o *Orchestrator) Register(name string, ch Channel) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.channels[name] = ch
}

// StartAll starts all registered channels and begins consuming their events.
// Each channel's Events() channel is consumed in its own goroutine. The
// provided context controls the lifetime of all consumers; StopAll cancels it.
// Idempotent per channel: a channel that is already running is left untouched.
func (o *Orchestrator) StartAll(ctx context.Context) {
	o.lifecycle.Lock()
	defer o.lifecycle.Unlock()

	// Derive a cancellable context so StopAll can signal all consume loops to
	// exit even when the caller passed context.Background() (as the GUI boot
	// does). Without this, channels whose Stop() doesn't close their Events()
	// channel (e.g. web) would leave their consume goroutine blocked forever.
	// A second StartAll reuses the existing root so an earlier caller's context
	// keeps governing the channels it already started.
	o.mu.Lock()
	if o.rootCancel == nil {
		rootCtx, cancel := context.WithCancel(ctx)
		o.rootCtx = rootCtx
		o.rootCancel = cancel
	}
	root := o.rootCtx
	names := o.channelNamesLocked()
	o.mu.Unlock()

	started := 0
	for _, name := range names {
		if err := o.startChannelLocked(root, name); err != nil {
			o.log.Error("channel start failed", "name", name, "error", err)
			continue
		}
		started++
	}
	o.log.Info("channel orchestrator started", "count", started, "registered", len(names))
}

// StartChannel starts a single registered channel and begins consuming its
// events. It is idempotent: starting an already-running channel is a no-op.
// The derived context is owned by the channel, so stopping it later does not
// affect any other channel.
func (o *Orchestrator) StartChannel(ctx context.Context, name string) error {
	o.lifecycle.Lock()
	defer o.lifecycle.Unlock()
	return o.startChannelLocked(ctx, name)
}

// StopChannel stops a single running channel and waits for its consume
// goroutine to exit. It is idempotent: stopping a channel that is not running
// succeeds without calling channel Stop a second time.
func (o *Orchestrator) StopChannel(name string) error {
	o.lifecycle.Lock()
	defer o.lifecycle.Unlock()
	return o.stopChannelLocked(name)
}

// Running returns the names of the channels that are currently running,
// sorted for a deterministic UI/status output.
func (o *Orchestrator) Running() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	names := make([]string, 0, len(o.runs))
	for name := range o.runs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsRunning reports whether the named channel is currently running.
func (o *Orchestrator) IsRunning(name string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.runs[name]
	return ok
}

// StopAll stops all running channels and waits for their event consumers to
// finish. Channels that were never started are not stopped, so a second
// StopAll is a no-op rather than a double Stop.
func (o *Orchestrator) StopAll() {
	o.lifecycle.Lock()
	defer o.lifecycle.Unlock()

	// Cancel the consume-loop contexts first so every consume goroutine's
	// <-ctx.Done() branch fires and exits, even for channels whose Stop()
	// does not close their Events() channel (e.g. web).
	o.mu.Lock()
	cancel := o.rootCancel
	o.rootCancel = nil
	o.rootCtx = nil
	runs := make([]*channelRun, 0, len(o.runs))
	for _, run := range o.runs {
		runs = append(runs, run)
	}
	o.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, run := range runs {
		run.cancel()
	}
	for _, run := range runs {
		if err := run.ch.Stop(); err != nil {
			o.log.Warn("channel stop failed", "name", run.name, "error", err)
		}
	}
	o.wg.Wait()

	// The consume goroutines remove their own entries on exit; drop anything
	// left over (e.g. a consumer that is still draining) so Running() is honest.
	o.mu.Lock()
	for _, run := range runs {
		if o.runs[run.name] == run {
			delete(o.runs, run.name)
		}
	}
	o.mu.Unlock()

	o.log.Info("channel orchestrator stopped")
}

// startChannelLocked starts one channel. Callers must hold o.lifecycle.
func (o *Orchestrator) startChannelLocked(parent context.Context, name string) error {
	o.mu.Lock()
	if _, running := o.runs[name]; running {
		o.mu.Unlock()
		return nil // idempotent: already running
	}
	ch, ok := o.channels[name]
	o.mu.Unlock()
	if !ok {
		return fmt.Errorf("channel %q not registered", name)
	}

	runCtx, cancel := context.WithCancel(parent)
	if err := ch.Start(runCtx); err != nil {
		cancel()
		return err
	}

	run := &channelRun{name: name, ch: ch, cancel: cancel, done: make(chan struct{})}
	o.mu.Lock()
	if _, exists := o.runs[name]; exists { // defensive: lifecycle serializes this
		o.mu.Unlock()
		cancel()
		_ = ch.Stop()
		return nil
	}
	o.runs[name] = run
	o.mu.Unlock()

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer o.finishRun(run)
		o.consume(runCtx, name, ch)
	}()

	o.log.Info("channel started", "name", name)
	return nil
}

// stopChannelLocked stops one running channel. Callers must hold o.lifecycle.
func (o *Orchestrator) stopChannelLocked(name string) error {
	o.mu.Lock()
	run, ok := o.runs[name]
	if ok {
		delete(o.runs, name)
	}
	o.mu.Unlock()
	if !ok {
		return nil // idempotent: not running
	}

	run.cancel()
	stopErr := run.ch.Stop()
	<-run.done
	if stopErr != nil {
		return fmt.Errorf("stop channel %q: %w", name, stopErr)
	}
	o.log.Info("channel stopped", "name", name)
	return nil
}

// finishRun removes a run's bookkeeping and unblocks any StopChannel waiting
// on it. It must be called exactly once, by the consume goroutine.
func (o *Orchestrator) finishRun(run *channelRun) {
	o.mu.Lock()
	if o.runs[run.name] == run {
		delete(o.runs, run.name)
	}
	o.mu.Unlock()
	close(run.done)
}

// channelNamesLocked returns the registered channel names. Callers must hold o.mu.
func (o *Orchestrator) channelNamesLocked() []string {
	names := make([]string, 0, len(o.channels))
	for name := range o.channels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

const (
	maxRestarts      = 5
	restartBaseDelay = 2 * time.Second
	restartMaxDelay  = 60 * time.Second
)

func (o *Orchestrator) consume(ctx context.Context, name string, ch Channel) {
	var consecutiveFailures int
	for {
		events := ch.Events()
		select {
		case msg, ok := <-events:
			if !ok {
				// Channel closed — attempt restart.
				consecutiveFailures++
				if consecutiveFailures > maxRestarts {
					o.log.Error("channel restart limit exceeded", "name", name, "failures", consecutiveFailures)
					return
				}
				delay := restartBaseDelay * time.Duration(1<<(consecutiveFailures-1))
				if delay > restartMaxDelay {
					delay = restartMaxDelay
				}
				o.log.Warn("channel events closed, restarting", "name", name, "attempt", consecutiveFailures, "delay", delay)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return
				}
				if err := ch.Start(ctx); err != nil {
					o.log.Error("channel restart failed", "name", name, "error", err)
					continue
				}
				o.log.Info("channel restarted", "name", name)
				continue
			}
			consecutiveFailures = 0 // reset on successful message
			// Typing indicator with periodic refresh, matching Rust's
			// spawn_scoped_typing_task (4s interval, cancellation on turn end).
			var stopTyping func()
			if ts, ok := ch.(TypingSender); ok && msg.From != "" {
				refreshCtx, cancelRefresh := context.WithCancel(ctx)
				_ = ts.StartTyping(refreshCtx, msg.From)
				go func() {
					ticker := time.NewTicker(4 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-refreshCtx.Done():
							return
						case <-ticker.C:
							_ = ts.StartTyping(refreshCtx, msg.From)
						}
					}
				}()
				stopTyping = func() {
					cancelRefresh()
					_ = ts.StopTyping(ctx, msg.From)
				}
			}
			response, derr := o.dispatcher.Handle(ctx, msg)
			if stopTyping != nil {
				stopTyping()
			}
			// Deliver the agent's reply back to the originating channel.
			// Skip empty responses (e.g. blocked messages) and log send
			// failures without failing the consumer loop.
			if derr == nil && strings.TrimSpace(response) != "" {
				reply := Message{
					ID: msg.ID + "-reply", Channel: msg.Channel, From: msg.From,
					Content: response, ReplyTo: msg.ReplyTo,
				}
				if err := ch.Send(ctx, reply); err != nil {
					o.log.Warn("failed to send reply to channel",
						"name", name, "error", err)
				}
			}
		case <-ctx.Done():
			return
		}
	}
}
