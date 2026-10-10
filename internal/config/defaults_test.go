package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	pkgtools "github.com/simon/mneme/pkg/tools"
)

// clearSecretEnv removes the provider API-key env vars so a test observes only
// the config file it wrote. t.Setenv registers the restore automatically.
func clearSecretEnv(t *testing.T) {
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

// TestEndpointDefaultConstants pins the byte-identical values of the shared
// endpoint constants (SET 1.1) and proves the config defaults reference them
// instead of repeating the literals.
func TestEndpointDefaultConstants(t *testing.T) {
	if DefaultInferenceHTTPPort != 8080 {
		t.Errorf("DefaultInferenceHTTPPort = %d, want 8080", DefaultInferenceHTTPPort)
	}
	if DefaultWebhookPort != 9500 {
		t.Errorf("DefaultWebhookPort = %d, want 9500", DefaultWebhookPort)
	}
	if DefaultLoopbackBind != "127.0.0.1" {
		t.Errorf("DefaultLoopbackBind = %q, want 127.0.0.1", DefaultLoopbackBind)
	}
	if DefaultOllamaBaseURL != "http://localhost:11434" {
		t.Errorf("DefaultOllamaBaseURL = %q, want http://localhost:11434", DefaultOllamaBaseURL)
	}
	if DefaultOpenAIBaseURL != "https://api.openai.com/v1" {
		t.Errorf("DefaultOpenAIBaseURL = %q, want https://api.openai.com/v1", DefaultOpenAIBaseURL)
	}

	cfg := DefaultConfig()
	if got := defaultInferenceHTTPConfig(); got.Port != DefaultInferenceHTTPPort || got.Bind != DefaultLoopbackBind {
		t.Errorf("defaultInferenceHTTPConfig = %+v, want port=%d bind=%q",
			got, DefaultInferenceHTTPPort, DefaultLoopbackBind)
	}
	if cfg.Webhook.Port != DefaultWebhookPort {
		t.Errorf("default webhook port = %d, want %d", cfg.Webhook.Port, DefaultWebhookPort)
	}
	for _, p := range cfg.Providers {
		if p.Name == "ollama" && p.BaseURL != DefaultOllamaBaseURL {
			t.Errorf("default ollama provider base URL = %q, want %q", p.BaseURL, DefaultOllamaBaseURL)
		}
	}
}

// TestToolTimeoutDefaultMatchesPkgTools is the SET 1.2 parity guard: the config
// default and the pkg/tools constant are the same number.
func TestToolTimeoutDefaultMatchesPkgTools(t *testing.T) {
	if pkgtools.DefaultTimeoutSecs != 120 {
		t.Fatalf("pkgtools.DefaultTimeoutSecs = %d, want 120", pkgtools.DefaultTimeoutSecs)
	}
	if got := defaultAgentLimits().DefaultToolTimeout; got != pkgtools.DefaultTimeoutSecs {
		t.Errorf("config default tool timeout = %d, want pkgtools.DefaultTimeoutSecs (%d)",
			got, pkgtools.DefaultTimeoutSecs)
	}
}

// TestScreenCaptureIntervalDefault documents the config-side screen capture
// cadence and asserts it is non-zero.
//
// internal/desktop/screen_intel.go substitutes a hardcoded 30s when the interval
// is zero, and exports no constant for it. Because internal/config must not
// import internal/desktop, that value cannot be referenced here; this test can
// only pin the config default (which is what the runtime actually supplies). A
// zero config default would silently fall through to the desktop 30s fallback.
func TestScreenCaptureIntervalDefault(t *testing.T) {
	if DefaultScreenCaptureIntervalSecs != 5 {
		t.Errorf("DefaultScreenCaptureIntervalSecs = %d, want the documented 5",
			DefaultScreenCaptureIntervalSecs)
	}
	if got := defaultScreenIntelligenceConfig().CaptureIntervalSecs; got != DefaultScreenCaptureIntervalSecs {
		t.Errorf("screen capture default = %d, want %d", got, DefaultScreenCaptureIntervalSecs)
	}
	if got := DefaultConfig().ScreenIntelligence.CaptureIntervalSecs; got == 0 {
		t.Error("config screen capture interval is 0; the desktop loop would fall back to its own 30s default")
	}
}

// TestResolveEmbeddingConfig_MatchesByNameAndModel covers the provider-matching
// rule: exact name first, then the provider whose model list contains the
// embedding model, and explicit embedding values always winning.
func TestResolveEmbeddingConfig_MatchesByNameAndModel(t *testing.T) {
	named := ProviderConfig{Name: "corp-embed", Type: "openai", APIKey: "sk-corp", BaseURL: "https://embed.corp.example/v1"}
	byModel := ProviderConfig{Name: "gateway", Type: "openai", APIKey: "sk-gw", BaseURL: "https://gw.example/v1",
		Models: []string{"text-embedding-3-large"}}

	tests := []struct {
		name         string
		providers    []ProviderConfig
		embedding    EmbeddingConfig
		wantKey      string
		wantBaseURL  string
		wantExplicit bool
	}{
		{
			name:      "name match",
			providers: []ProviderConfig{named},
			embedding: EmbeddingConfig{Provider: "corp-embed", Model: "corp-embed-3"},
			wantKey:   "sk-corp", wantBaseURL: "https://embed.corp.example/v1",
		},
		{
			name:      "name match is case-insensitive",
			providers: []ProviderConfig{named},
			embedding: EmbeddingConfig{Provider: "CORP-EMBED"},
			wantKey:   "sk-corp", wantBaseURL: "https://embed.corp.example/v1",
		},
		{
			name:      "model match",
			providers: []ProviderConfig{named, byModel},
			embedding: EmbeddingConfig{Provider: "openai", Model: "text-embedding-3-large"},
			wantKey:   "sk-gw", wantBaseURL: "https://gw.example/v1",
		},
		{
			name:      "explicit embedding values win",
			providers: []ProviderConfig{named},
			embedding: EmbeddingConfig{Provider: "corp-embed", APIKey: "sk-explicit", BaseURL: "https://explicit.example/v1"},
			wantKey:   "sk-explicit", wantBaseURL: "https://explicit.example/v1", wantExplicit: true,
		},
		{
			name:      "no match leaves the endpoint unset",
			providers: []ProviderConfig{named},
			embedding: EmbeddingConfig{Provider: "openai", Model: "text-embedding-3-small"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Providers = tt.providers
			cfg.Embedding = tt.embedding

			got := cfg.ResolveEmbeddingConfig()
			if got.ResolvedAPIKey() != tt.wantKey {
				t.Errorf("ResolvedAPIKey() = %q, want %q", got.ResolvedAPIKey(), tt.wantKey)
			}
			if got.ResolvedBaseURL() != tt.wantBaseURL {
				t.Errorf("ResolvedBaseURL() = %q, want %q", got.ResolvedBaseURL(), tt.wantBaseURL)
			}
			if tt.wantExplicit {
				if got.resolvedAPIKey != "" || got.resolvedBaseURL != "" {
					t.Errorf("resolved fields set despite explicit values: %+v", got)
				}
			}
		})
	}
}

// TestLoad_ResolvesEmbeddingFromTOMLOnlyProvider is the SET 1.7 behaviour test:
// a TOML-only provider api_key/base_url must become visible to the embedder
// through the loaded config.
func TestLoad_ResolvesEmbeddingFromTOMLOnlyProvider(t *testing.T) {
	clearSecretEnv(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `[embedding]
provider = "corp-embed"
model = "corp-embed-3"

[[providers]]
name = "corp-embed"
type = "openai"
api_key = "sk-corp-only-in-toml"
base_url = "https://embed.corp.example/v1"
models = ["corp-embed-3"]
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The explicit embedding fields stay empty — resolution lives in the
	// derived (unexported) fields, which the TOML encoder cannot persist.
	if cfg.Embedding.APIKey != "" || cfg.Embedding.BaseURL != "" {
		t.Fatalf("explicit embedding fields were mutated: %+v", cfg.Embedding)
	}
	if got := cfg.Embedding.ResolvedAPIKey(); got != "sk-corp-only-in-toml" {
		t.Errorf("ResolvedAPIKey() = %q, want sk-corp-only-in-toml", got)
	}
	if got := cfg.Embedding.ResolvedBaseURL(); got != "https://embed.corp.example/v1" {
		t.Errorf("ResolvedBaseURL() = %q, want https://embed.corp.example/v1", got)
	}
}

// TestConfig_SaveDoesNotPersistResolvedEmbeddingKey guards against the
// resolution leaking a provider (possibly env-injected) key into the
// [embedding] section on save.
func TestConfig_SaveDoesNotPersistResolvedEmbeddingKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := DefaultConfig()
	cfg.Providers = []ProviderConfig{{
		Name: "corp-embed", Type: "openai",
		APIKey: "sk-must-not-reach-embedding", BaseURL: "https://embed.corp.example/v1",
	}}
	cfg.Embedding = EmbeddingConfig{Provider: "corp-embed"}
	cfg.Embedding = cfg.ResolveEmbeddingConfig()
	if cfg.Embedding.ResolvedAPIKey() != "sk-must-not-reach-embedding" {
		t.Fatalf("resolution did not happen: %+v", cfg.Embedding)
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var persisted struct {
		Embedding struct {
			APIKey  string `toml:"api_key"`
			BaseURL string `toml:"base_url"`
		} `toml:"embedding"`
	}
	if _, err := toml.DecodeFile(path, &persisted); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if persisted.Embedding.APIKey != "" {
		t.Errorf("resolved embedding api_key was persisted: %q", persisted.Embedding.APIKey)
	}
	if persisted.Embedding.BaseURL != "" {
		t.Errorf("resolved embedding base_url was persisted: %q", persisted.Embedding.BaseURL)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	embeddingSection := string(raw)
	if i := strings.Index(embeddingSection, "[embedding]"); i >= 0 {
		embeddingSection = embeddingSection[i:]
		if j := strings.Index(embeddingSection, "\n["); j >= 0 {
			embeddingSection = embeddingSection[:j]
		}
		if strings.Contains(embeddingSection, "sk-must-not-reach-embedding") {
			t.Errorf("resolved key leaked into the [embedding] section:\n%s", embeddingSection)
		}
	}
}
