package memory

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/simon/mneme/internal/config"

	"github.com/simon/mneme/internal/embeddings"
	embproviders "github.com/simon/mneme/internal/embeddings/providers"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/memory/archivist"
)

// SetupEmbedder creates an embedding provider and wires it into the pipeline.
// cfg.Provider: "ollama", "openai", or "" to disable.
func SetupEmbedder(p *Pipeline, cfg config.EmbeddingConfig, log *slog.Logger) {
	if cfg.Provider == "" {
		return
	}
	baseURL, apiKey := resolveEmbeddingEndpoint(cfg)
	var embedder embeddings.Provider
	switch cfg.Provider {
	case "ollama":
		embedder = embproviders.NewOllama(baseURL, cfg.Model)
	case "openai":
		embedder = embproviders.NewOpenAI(apiKey, baseURL, cfg.Model)
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

// resolveEmbeddingEndpoint returns the effective base URL and API key for the
// configured embedding provider, in priority order:
//
//  1. explicit embedding.api_key / embedding.base_url values;
//  2. values resolved at config load from a matching [[providers]] entry
//     (config.Config.ResolveEmbeddingConfig), which is what makes a TOML-only
//     provider key/base URL visible to the embedder;
//  3. provider-specific fallbacks — the loopback Ollama URL, then the
//     OPENAI_API_KEY env var for the openai provider (kept for deployments
//     that set only the env var and no provider/embedding section).
func resolveEmbeddingEndpoint(cfg config.EmbeddingConfig) (baseURL, apiKey string) {
	baseURL = cfg.ResolvedBaseURL()
	apiKey = cfg.ResolvedAPIKey()
	if baseURL == "" && cfg.Provider == "ollama" {
		baseURL = config.DefaultOllamaBaseURL
	}
	if apiKey == "" && cfg.Provider == "openai" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return baseURL, apiKey
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
