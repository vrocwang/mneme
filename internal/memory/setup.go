package memory

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/simon/mneme/internal/embeddings"
	embproviders "github.com/simon/mneme/internal/embeddings/providers"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/memory/archivist"
)

// SetupEmbedder creates an embedding provider and wires it into the pipeline.
// providerType: "ollama", "openai", or "" to disable.
func SetupEmbedder(p *Pipeline, providerType, baseURL, apiKey string, log *slog.Logger) {
	if providerType == "" {
		return
	}
	var embedder embeddings.Provider
	switch providerType {
	case "ollama":
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		embedder = embproviders.NewOllama(baseURL)
	case "openai":
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		embedder = embproviders.NewOpenAI(apiKey)
	}
	if embedder != nil {
		p.WithEmbedder(embeddings.NewBatchedProvider(embedder, 200*time.Millisecond, 8))
		log.Info("embeddings provider enabled", "provider", embedder.Name())

		// Backfill existing chunks that were stored without vectors.
		go func() {
			count, err := p.ReembedBackfill(context.Background())
			if err != nil {
				log.Warn("embedding backfill failed", "error", err)
			} else if count > 0 {
				log.Info("embedding backfill complete", "chunks_updated", count)
			}
		}()
	}
}

// SetupArchivist creates an LLM-based archivist and wires it into the pipeline.
// provider and model are used for LLM summarization/deduplication/entity extraction.
func SetupArchivist(p *Pipeline, provider inference.Provider, model string, log *slog.Logger) {
	if provider == nil {
		return
	}
	a := archivist.New(log, provider, model)
	p.WithArchivist(a)
	log.Info("archivist enabled for memory pipeline", "model", model)
}
