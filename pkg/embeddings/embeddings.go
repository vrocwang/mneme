// Package embeddings defines the shared Provider interface used by both the
// main Mneme binary and externally-built extensions.
package embeddings

import "context"

// Provider generates embeddings for text.
type Provider interface {
	Name() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}
