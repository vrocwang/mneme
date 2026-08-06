package channels

import (
	"context"
	"fmt"
	"log/slog"
)

// DispatchFunc is the callback that processes a channel message through the
// agent and returns the response. Provided by app.go at wiring time.
type DispatchFunc func(ctx context.Context, msg Message) (string, error)

// Dispatcher receives messages from all channels and routes them to the
// agent through the provided dispatch function.
type Dispatcher struct {
	log        *slog.Logger
	dispatchFn DispatchFunc
}

// NewDispatcher creates a channel message dispatcher.
func NewDispatcher(log *slog.Logger, dispatchFn DispatchFunc) *Dispatcher {
	return &Dispatcher{log: log, dispatchFn: dispatchFn}
}

// Handle processes an inbound channel message. It sanitizes the content,
// dispatches to the agent, and returns the agent's response so the caller
// (orchestrator) can deliver it back to the originating channel.
func (d *Dispatcher) Handle(ctx context.Context, msg Message) (string, error) {
	// Sanitize before dispatch.
	sanitized, blocked := SanitizeInbound(msg.Content, d.log)
	if blocked {
		d.log.Warn("channel message blocked by injection detection",
			"channel", msg.Channel, "from", msg.From)
		return "", nil
	}

	d.log.Info("dispatching channel message",
		"channel", msg.Channel, "from", msg.From,
		"content_len", len(sanitized))

	if d.dispatchFn == nil {
		d.log.Warn("no dispatch function set, message dropped",
			"channel", msg.Channel)
		return "", nil
	}

	msg.Content = sanitized
	response, err := d.dispatchFn(ctx, msg)
	if err != nil {
		d.log.Error("channel dispatch failed",
			"channel", msg.Channel, "error", err)
		return "", err
	}
	d.log.Debug("channel dispatch complete",
		"channel", msg.Channel, "response_len", len(response))
	return response, nil
}

// ChannelThreadID derives a stable thread identifier for a channel+ sender pair.
func ChannelThreadID(channel, sender string) string {
	return fmt.Sprintf("channel-%s-%s", channel, sender)
}

// DispatchToChatService returns a DispatchFunc that routes channel messages
// to a ChatService-like interface. Keeps app.go free of dispatch logic.
func DispatchToChatService(svc ChatMessageSender) DispatchFunc {
	return func(ctx context.Context, msg Message) (string, error) {
		threadID := ChannelThreadID(msg.Channel, msg.From)
		// Inject channel metadata so the agent knows the message origin.
		enriched := msg.Content
		if msg.Channel != "" && msg.Channel != "web" {
			if msg.From != "" {
				enriched = fmt.Sprintf("[From: @%s via %s]\n%s", msg.From, msg.Channel, msg.Content)
			} else {
				enriched = fmt.Sprintf("[via %s]\n%s", msg.Channel, msg.Content)
			}
		}
		result, err := svc.Send(ctx, threadID, enriched)
		if err != nil {
			return "", err
		}
		return result, nil
	}
}

// ChatMessageSender is the interface the dispatcher needs from the chat service.
type ChatMessageSender interface {
	Send(ctx context.Context, threadID, message string) (string, error)
}
