package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon/mneme/internal/config"
)

// clearEmbeddingEnv removes provider/embedding env vars so a test observes only
// the config file it wrote. t.Setenv registers the restore automatically.
func clearEmbeddingEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "DEEPSEEK_API_KEY",
		"MNEME_CUSTOM_API_KEY", "MNEME_CUSTOM_BASE_URL",
		"MNEME_EMBEDDING_PROVIDER", "MNEME_EMBEDDING_MODEL",
		"MNEME_EMBEDDING_BASE_URL", "MNEME_EMBEDDING_API_KEY",
	} {
		t.Setenv(k, "")
	}
}

func writeEmbeddingConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestResolveEmbeddingEndpoint_TOMLOnlyProvider is the end-to-end SET 1.7 test:
// a provider configured only in TOML must reach the embedder, instead of the
// embedder falling back to the raw OPENAI_API_KEY env var (which is unset here).
func TestResolveEmbeddingEndpoint_TOMLOnlyProvider(t *testing.T) {
	clearEmbeddingEnv(t)

	path := writeEmbeddingConfig(t, `[embedding]
provider = "corp-embed"
model = "corp-embed-3"

[[providers]]
name = "corp-embed"
type = "openai"
api_key = "sk-toml-only"
base_url = "https://embed.corp.example/v1"
models = ["corp-embed-3"]
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	baseURL, apiKey := resolveEmbeddingEndpoint(cfg.Embedding)
	if apiKey != "sk-toml-only" {
		t.Errorf("apiKey = %q, want sk-toml-only (from the TOML provider)", apiKey)
	}
	if baseURL != "https://embed.corp.example/v1" {
		t.Errorf("baseURL = %q, want https://embed.corp.example/v1 (from the TOML provider)", baseURL)
	}
}

// TestResolveEmbeddingEndpoint_ExplicitFieldsWin verifies the embedding section
// still takes precedence over a matching provider entry.
func TestResolveEmbeddingEndpoint_ExplicitFieldsWin(t *testing.T) {
	clearEmbeddingEnv(t)

	path := writeEmbeddingConfig(t, `[embedding]
provider = "corp-embed"
api_key = "sk-explicit"
base_url = "https://explicit.example/v1"

[[providers]]
name = "corp-embed"
type = "openai"
api_key = "sk-provider"
base_url = "https://provider.example/v1"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	baseURL, apiKey := resolveEmbeddingEndpoint(cfg.Embedding)
	if apiKey != "sk-explicit" {
		t.Errorf("apiKey = %q, want sk-explicit", apiKey)
	}
	if baseURL != "https://explicit.example/v1" {
		t.Errorf("baseURL = %q, want https://explicit.example/v1", baseURL)
	}
}

// TestResolveEmbeddingEndpoint_Fallbacks covers the provider-specific fallbacks
// used when neither the embedding section nor a provider supplied a value.
func TestResolveEmbeddingEndpoint_Fallbacks(t *testing.T) {
	t.Run("ollama falls back to the shared default URL", func(t *testing.T) {
		clearEmbeddingEnv(t)
		baseURL, apiKey := resolveEmbeddingEndpoint(config.EmbeddingConfig{Provider: "ollama"})
		if baseURL != config.DefaultOllamaBaseURL {
			t.Errorf("baseURL = %q, want %q", baseURL, config.DefaultOllamaBaseURL)
		}
		if apiKey != "" {
			t.Errorf("apiKey = %q, want empty for ollama", apiKey)
		}
	})

	t.Run("openai falls back to OPENAI_API_KEY", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-from-env")
		baseURL, apiKey := resolveEmbeddingEndpoint(config.EmbeddingConfig{Provider: "openai"})
		if apiKey != "sk-from-env" {
			t.Errorf("apiKey = %q, want sk-from-env", apiKey)
		}
		if baseURL != "" {
			t.Errorf("baseURL = %q, want empty (the provider fills its own default)", baseURL)
		}
	})

	t.Run("openai without env key stays empty", func(t *testing.T) {
		clearEmbeddingEnv(t)
		baseURL, apiKey := resolveEmbeddingEndpoint(config.EmbeddingConfig{Provider: "openai"})
		if apiKey != "" || baseURL != "" {
			t.Errorf("got baseURL=%q apiKey=%q, want both empty", baseURL, apiKey)
		}
	})
}
