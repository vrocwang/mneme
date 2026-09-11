package channels

import "context"

// TypingSender is an optional interface a Channel may implement to publish
// typing indicators while a reply is being generated. The orchestrator
// type-asserts it; channels that do not support typing simply omit it.
type TypingSender interface {
	StartTyping(ctx context.Context, target string) error
	StopTyping(ctx context.Context, target string) error
}
