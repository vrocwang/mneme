package channels

import "context"

// Message represents an inbound/outbound channel message.
type Message struct {
	ID      string
	Channel string
	From    string
	Content string
	ReplyTo string // channel-specific reply target
}

// Channel is the interface all messaging channels implement.
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Send(ctx context.Context, msg Message) error
	Events() <-chan Message
}
