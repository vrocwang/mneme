package channels

import "context"

// Names of the always-available in-process channels. They identify a channel in
// Channel.Name, on Message.Channel, and in the channel list returned to the UI,
// so the literal must not drift between those places.
const (
	ChannelWeb = "web"
	ChannelCLI = "cli"
)

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
