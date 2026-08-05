package embeddings

import (
	"context"

	pkgemb "github.com/simon/mneme/pkg/embeddings"
)

// Re-export shared types from pkg/embeddings.
type Provider = pkgemb.Provider

// MockProvider for testing
type MockProvider struct {
	NameStr string
	Dim     int
	Vecs    [][]float32
}

func (m *MockProvider) Name() string {
	if m.NameStr == "" {
		return "mock"
	}
	return m.NameStr
}

func (m *MockProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.Vecs != nil {
		return m.Vecs, nil
	}
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, m.Dim)
		result[i][0] = float32(len(texts[i])) // deterministic for testing
	}
	return result, nil
}

func (m *MockProvider) Dimensions() int { return m.Dim }
