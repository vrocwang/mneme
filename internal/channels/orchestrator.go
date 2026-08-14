package channels

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Orchestrator manages the lifecycle of all channels and consumes their
// events, routing each message to the dispatcher for agent processing.
// Owned by the channels package so app.go stays a thin wiring layer.
type Orchestrator struct {
	log        *slog.Logger
	channels   map[string]Channel
	dispatcher *Dispatcher
	mu         sync.Mutex
	wg         sync.WaitGroup
	cancel     context.CancelFunc // cancels the consume-loop context on StopAll
}

// NewOrchestrator creates a channel orchestrator.
func NewOrchestrator(log *slog.Logger, dispatcher *Dispatcher) *Orchestrator {
	return &Orchestrator{
		log:        log,
		channels:   make(map[string]Channel),
		dispatcher: dispatcher,
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
func (o *Orchestrator) StartAll(ctx context.Context) {
	// Derive a cancellable context so StopAll can signal all consume loops to
	// exit even when the caller passed context.Background() (as the GUI boot
	// does). Without this, channels whose Stop() doesn't close their Events()
	// channel (e.g. web) would leave their consume goroutine blocked forever.
	ctx, cancel := context.WithCancel(ctx)
	o.mu.Lock()
	o.cancel = cancel
	chans := make(map[string]Channel, len(o.channels))
	for name, ch := range o.channels {
		chans[name] = ch
	}
	o.mu.Unlock()

	for name, ch := range chans {
		if err := ch.Start(ctx); err != nil {
			o.log.Error("channel start failed", "name", name, "error", err)
			continue
		}
		o.log.Info("channel started", "name", name)

		o.wg.Add(1)
		go func(name string, ch Channel) {
			defer o.wg.Done()
			o.consume(ctx, name, ch)
		}(name, ch)
	}
	o.log.Info("channel orchestrator started", "count", len(chans))
}

// StopAll stops all channels and waits for event consumers to finish.
func (o *Orchestrator) StopAll() {
	// Cancel the consume-loop context first so every consume goroutine's
	// <-ctx.Done() branch fires and exits, even for channels whose Stop()
	// does not close their Events() channel (e.g. web).
	o.mu.Lock()
	cancel := o.cancel
	chans := make(map[string]Channel, len(o.channels))
	for name, ch := range o.channels {
		chans[name] = ch
	}
	o.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	for name, ch := range chans {
		if err := ch.Stop(); err != nil {
			o.log.Warn("channel stop failed", "name", name, "error", err)
		}
	}
	o.wg.Wait()
	o.log.Info("channel orchestrator stopped")
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
