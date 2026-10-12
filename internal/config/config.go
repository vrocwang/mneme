package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/simon/mneme/internal/security"
	workspace "github.com/simon/mneme/internal/workspace"
)

// ── Shared endpoint defaults ─────────────────────────────────────────────
//
// These constants are the single source of truth for endpoints and ports that
// are also consumed by the wiring packages (internal/jsonrpc,
// internal/webhooks, internal/embeddings, internal/voice, internal/memory).
// Each was previously written twice — once as a config default and once as a
// fallback in the consumer — so the two could drift. The import direction is
// one-way: those packages import internal/config, never the reverse.

const (
	// DefaultInferenceHTTPPort is the loopback port the inference HTTP server
	// binds when config does not specify one.
	DefaultInferenceHTTPPort = 8080

	// DefaultWebhookPort is the loopback port the inbound webhook listener
	// binds when config does not specify one.
	DefaultWebhookPort = 9500

	// DefaultLoopbackBind is the bind address shared by the local-only HTTP
	// surfaces (inference HTTP server and webhook listener).
	DefaultLoopbackBind = "127.0.0.1"

	// DefaultOllamaBaseURL is the default Ollama endpoint: the built-in
	// "ollama" provider entry and the Ollama embedding provider both use it.
	DefaultOllamaBaseURL = "http://localhost:11434"

	// DefaultOpenAIBaseURL is the default endpoint for the OpenAI API and for
	// every OpenAI-compatible client (LLM providers, embeddings, TTS, STT).
	DefaultOpenAIBaseURL = "https://api.openai.com/v1"
)

// ProviderConfig defines an LLM provider in the config file.
type ProviderConfig struct {
	Name    string   `toml:"name" json:"name"`
	Type    string   `toml:"type" json:"type"` // "openai", "anthropic", "ollama"
	APIKey  string   `toml:"api_key" json:"api_key"`
	BaseURL string   `toml:"base_url" json:"base_url"`
	Models  []string `toml:"models" json:"models"` // model names this provider supports
}

// ChannelConfig holds credentials for a messaging channel. The map key is
// the channel name; consumers (core web/cli and channel extensions) read
// the fields they need and ignore the rest.
type ChannelConfig struct {
	Disabled   bool              `toml:"disabled"` // generic off switch, honored by channel consumers
	Token      string            `toml:"token"`
	WebhookURL string            `toml:"webhook_url"`
	Extra      map[string]string `toml:"extra"` // provider-specific credentials by key
}

// MCPServerEntry mirrors the registry's ServerEntry for TOML serialization.
type MCPServerEntry struct {
	Name      string   `toml:"name"`
	Transport string   `toml:"transport"` // "stdio" or "http"
	Command   string   `toml:"command"`
	Args      []string `toml:"args"`
	URL       string   `toml:"url"`
	Enabled   bool     `toml:"enabled"`
}

type Config struct {
	Workspace          string                   `toml:"workspace"` // root for all data: config.toml, DB, extensions, logs, etc.
	SchemaVersion      int                      `toml:"schema_version"`
	Agent              AgentConfig              `toml:"agent"`
	Security           SecurityConfig           `toml:"security"`
	Providers          []ProviderConfig         `toml:"providers"`
	Channels           map[string]ChannelConfig `toml:"channels"`
	MCPServers         []MCPServerEntry         `toml:"mcp_servers"`
	Search             SearchConfig             `toml:"search"`
	Memory             MemoryConfig             `toml:"memory"`
	Embedding          EmbeddingConfig          `toml:"embedding"`
	Voice              VoiceConfig              `toml:"voice"`
	Webhook            WebhookConfig            `toml:"webhook"`
	Learning           LearningConfig           `toml:"learning_cfg"`
	Cron               CronConfig               `toml:"cron"`
	Tools              ToolsConfig              `toml:"tools"`
	CircuitBreaker     CircuitBreakerConfig     `toml:"circuit_breaker"`
	Cost               CostConfig               `toml:"cost"`
	Autonomy           AutonomyConfig           `toml:"autonomy"`
	Sandbox            SandboxConfig            `toml:"sandbox"`
	ActionDir          ActionDirConfig          `toml:"action_dir"`
	Observability      ObservabilityConfig      `toml:"observability"`
	Proxy              ProxyConfig              `toml:"proxy"`
	ScreenIntelligence ScreenIntelligenceConfig `toml:"screen_intelligence"`
	InferenceHTTP      InferenceHTTPConfig      `toml:"inference_http"`
	Bundles            BundleConfig             `toml:"bundles"`
}

// BundleConfig controls which capability bundles are enabled at boot. Bundles
// are the no-privileged-core composition units: the built-in agents, core
// tools, network tools, productivity tools, and event-driven behaviors
// (post-turn hooks, cron jobs, subconscious evaluators, heartbeat) are each a
// named bundle that can be disabled.
type BundleConfig struct {
	// Disabled lists bundle IDs to skip at boot. Empty = all enabled.
	Disabled []string `toml:"disabled" json:"disabled"`
}

// SearchConfig holds API keys and endpoints for web search engines.
type SearchConfig struct {
	BraveAPIKey  string `toml:"brave_api_key"`
	TavilyAPIKey string `toml:"tavily_api_key"`
	SearxNGURL   string `toml:"searxng_url"`
}

// MemoryConfig holds settings for the memory pipeline.
type MemoryConfig struct {
	MaxChunkSize     int                    `toml:"max_chunk_size"`     // max chars per memory chunk, 0 = default
	RetentionDays    int                    `toml:"retention_days"`     // auto-purge chunks older than N days, 0 = keep forever
	MaxSearchResults int                    `toml:"max_search_results"` // max results returned by memory search, 0 = default
	Pipeline         MemoryPipelineConfig   `toml:"pipeline"`
	RetrievalWeights RetrievalWeightsConfig `toml:"retrieval_weights"`
}

// EmbeddingConfig selects and configures the embedding provider.
type EmbeddingConfig struct {
	Provider string `toml:"provider"` // "ollama", "openai", or "" to disable
	Model    string `toml:"model"`    // model name, empty = provider default
	BaseURL  string `toml:"base_url"` // override API base URL
	APIKey   string `toml:"api_key"`  // API key (OpenAI)

	// resolvedAPIKey and resolvedBaseURL carry the provider-derived endpoint
	// resolved at load time (see Config.ResolveEmbeddingConfig). They are
	// deliberately unexported so the TOML encoder never writes them back: a
	// key that came from the environment would otherwise be persisted to
	// config.toml and then shadow a later rotation of that env var.
	resolvedAPIKey  string
	resolvedBaseURL string
}

// ResolvedAPIKey returns the effective API key: the explicitly configured
// value, else the provider-derived value resolution found, else "".
func (e EmbeddingConfig) ResolvedAPIKey() string {
	if e.APIKey != "" {
		return e.APIKey
	}
	return e.resolvedAPIKey
}

// ResolvedBaseURL returns the effective base URL: the explicitly configured
// value, else the provider-derived value resolution found, else "".
func (e EmbeddingConfig) ResolvedBaseURL() string {
	if e.BaseURL != "" {
		return e.BaseURL
	}
	return e.resolvedBaseURL
}

// ToolsConfig holds configuration for tool implementations.
type ToolsConfig struct {
	Shell         ToolsShellConfig `toml:"shell"`
	OptionalTools []string         `toml:"optional_tools"` // empty=all (backward compat), non-empty=only listed, ["none"]=disable all
}

// VoiceConfig holds settings for speech-to-text and text-to-speech.
type VoiceConfig struct {
	// STTProvider selects the speech-to-text engine: "system", "whisper", or "openai".
	STTProvider string `toml:"stt_provider"`
	// STTModel is the model name for the selected STT provider (e.g. "whisper-1", "base").
	STTModel string `toml:"stt_model"`
	// STTEndpoint overrides the API base URL for cloud STT providers.
	STTEndpoint string `toml:"stt_endpoint"`
	// STTAPIKey is a dedicated API key for the STT provider. When empty, the
	// factory falls back to a provider-configured key matching the engine type.
	STTAPIKey string `toml:"stt_api_key"`

	// TTSProvider selects the text-to-speech engine: "system", "piper", or "openai".
	TTSProvider string `toml:"tts_provider"`
	// TTSModel is the model:voice for the selected TTS provider (e.g. "tts-1:alloy", "en_US-lessac-medium").
	TTSModel string `toml:"tts_model"`
	// TTSEndpoint overrides the API base URL for cloud TTS providers.
	TTSEndpoint string `toml:"tts_endpoint"`
	// TTSAPIKey is a dedicated API key for the TTS provider. When empty, the
	// factory falls back to a provider-configured key matching the engine type.
	TTSAPIKey string `toml:"tts_api_key"`
}

// WebhookConfig holds settings for the inbound webhook server.
type WebhookConfig struct {
	Enabled bool   `toml:"enabled"` // start the local HTTP listener
	Port    int    `toml:"port"`    // HTTP listen port, default 9500
	Secret  string `toml:"secret"`  // HMAC secret for signature verification
}

// LearningConfig controls the post-turn learning engine.
type LearningConfig struct {
	Enabled bool `toml:"enabled"` // enable learning from conversations
}

// CronConfig holds settings for the cron scheduler.
type CronConfig struct {
	HeartbeatIntervalSecs int `toml:"heartbeat_interval_secs"` // seconds between heartbeat ticks, default 30
}

type AgentConfig struct {
	DefaultModel    string            `toml:"default_model"`     // model name — matched against provider models lists
	MaxOutputTokens int               `toml:"max_output_tokens"` // 0 = use provider default
	Temperature     float64           `toml:"temperature"`       // 0 = use provider default
	ModelRoutes     map[string]string `toml:"model_routes"`      // task kind → model override; see Config.ModelForRoute for live kinds
	Limits          AgentLimits       `toml:"limits"`
}

type SecurityConfig struct {
	Tier           string             `toml:"tier"`            // readonly | supervised | full
	WorkspaceOnly  bool               `toml:"workspace_only"`  // restrict all file access to workspace
	TrustedRoots   []TrustedRootEntry `toml:"trusted_roots"`   // directories outside workspace with explicit grants
	ForbiddenPaths []string           `toml:"forbidden_paths"` // additional paths to always block
	Commands       SecurityCommands   `toml:"commands"`
}

// TrustedRootEntry grants read or readwrite access to a directory outside the workspace.
type TrustedRootEntry struct {
	Path   string `toml:"path"`
	Access string `toml:"access"` // "read" or "readwrite"
}

func DefaultConfig() *Config {
	return &Config{
		Workspace: defaultWorkspaceDir(),
		Agent: AgentConfig{
			DefaultModel: "llama3",
			Limits:       defaultAgentLimits(),
		},
		Security: SecurityConfig{
			Tier:     string(security.TierSupervised),
			Commands: defaultSecurityCommands(),
		},
		Providers: []ProviderConfig{
			{
				Name:    "ollama",
				Type:    "ollama",
				BaseURL: DefaultOllamaBaseURL,
				Models:  []string{"llama3", "qwen2.5", "deepseek-r1"},
			},
			{
				Name:    "lmstudio",
				Type:    "openai",
				BaseURL: "http://localhost:1234/v1",
			},
		},
		Memory: MemoryConfig{
			Pipeline:         defaultMemoryPipelineConfig(),
			RetrievalWeights: DefaultRetrievalWeights(),
		},
		Tools: ToolsConfig{
			Shell: defaultToolsShellConfig(),
		},
		CircuitBreaker:     defaultCircuitBreakerConfig(),
		Cost:               defaultCostConfig(),
		Autonomy:           defaultAutonomyConfig(),
		Sandbox:            defaultSandboxConfig(),
		Observability:      defaultObservabilityConfig(),
		ScreenIntelligence: defaultScreenIntelligenceConfig(),
		InferenceHTTP:      defaultInferenceHTTPConfig(),
		Webhook: WebhookConfig{
			Port:    DefaultWebhookPort,
			Enabled: false,
		},
	}
}

// ExportWorkspaceEnv makes dir the workspace that child processes inherit by
// exporting it as MNEME_HOME. It is the single place that keeps the variable in
// sync with the active workspace: Load calls it on every load, and
// ConfigRPC.SetWorkspace calls it when the user relocates the workspace.
func ExportWorkspaceEnv(dir string) {
	if dir == "" {
		return
	}
	dir = filepath.Clean(dir)
	if os.Getenv("MNEME_HOME") == dir {
		return
	}
	if err := os.Setenv("MNEME_HOME", dir); err != nil {
		slog.Warn("could not export MNEME_HOME; extension subprocesses may use the wrong workspace",
			"dir", dir, "error", err)
		return
	}
	slog.Info("workspace exported for extension subprocesses", "MNEME_HOME", dir)
}

// WorkspaceDir returns the data directory alongside the executable.
func WorkspaceDir() string {
	// MNEME_HOME overrides the default (exe_dir/data) so users can relocate
	// the workspace. Exported to env by main.go for extensions to read.
	if dir := os.Getenv("MNEME_HOME"); dir != "" {
		return filepath.Clean(dir)
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

// TempDir returns the workspace's scratch directory.
func TempDir() string {
	dir := filepath.Join(WorkspaceDir(), workspace.DirTmp)
	os.MkdirAll(dir, 0755)
	return dir
}

func defaultWorkspaceDir() string {
	return WorkspaceDir()
}

// ProjectsDirEnvVar allows overriding the projects directory via the environment.
const ProjectsDirEnvVar = "MNEME_PROJECTS_DIR"

// ActionDirEnvVar allows overriding the action directory via the environment.
const ActionDirEnvVar = "MNEME_ACTION_DIR"

// ProjectsDir returns the user-facing projects workspace (for agent file access).
// Resolution: env MNEME_PROJECTS_DIR > <workspace>/projects
func (c *Config) ProjectsDir() string {
	if dir := os.Getenv(ProjectsDirEnvVar); dir != "" {
		return filepath.Clean(dir)
	}
	return workspace.ProjectsDir(c.Workspace)
}

// ResolveActionDir returns the effective agent tool sandbox root.
// Resolution: env MNEME_ACTION_DIR > config ActionDir.Override > ProjectsDir()
func (c *Config) ResolveActionDir() string {
	if dir := os.Getenv(ActionDirEnvVar); dir != "" {
		return filepath.Clean(dir)
	}
	if c.ActionDir.Override != "" {
		return filepath.Clean(c.ActionDir.Override)
	}
	return c.ProjectsDir()
}

// ConfigDir returns the config override directory (<workspace>/config).
func (c *Config) ConfigDir(sub ...string) string {
	return filepath.Join(append([]string{workspace.ConfigDir(c.Workspace)}, sub...)...)
}

// ExtensionConfigDir returns the directory for per-extension config files.
func (c *Config) ExtensionConfigDir() string {
	return filepath.Join(workspace.ConfigDir(c.Workspace), "extensions")
}

// LoadExtensionConfig reads a per-extension JSON config file.
func (c *Config) LoadExtensionConfig(name string) (map[string]interface{}, error) {
	path := filepath.Join(c.ExtensionConfigDir(), name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse extension config %s: %w", name, err)
	}
	return cfg, nil
}

// SaveExtensionConfig writes a per-extension JSON config file.
func (c *Config) SaveExtensionConfig(name string, cfg map[string]interface{}) error {
	dir := c.ExtensionConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal extension config %s: %w", name, err)
	}
	path := filepath.Join(dir, name+".json")
	return os.WriteFile(path, data, 0644)
}

// SecretsDir returns the secrets storage directory (<home>/secrets).
func (c *Config) SecretsDir() string {
	return workspace.SecretsDir(c.Workspace)
}

// ConfigPath returns the path to config.toml inside the given workspace.
func ConfigPath(root string) string {
	return filepath.Join(root, workspace.FileConfig)
}

// Load reads config from path. On first run, creates directory and writes defaults.
// Env-var secrets (API keys, tokens) are merged AFTER the first-run save so they
// are never persisted to disk in plaintext.
// Load reads the config file (creating it with defaults when absent), applies
// environment overrides, and makes the resolved workspace the process's
// MNEME_HOME so child processes — extension subprocesses read it through
// extsdk.WorkspaceDir() — operate on the same root as the core. Without this a
// `workspace` value in the file (or a later SetWorkspace) would leave
// extensions pointed at the old root, and headless modes, which never set the
// variable, would let them fall back to the binary's directory.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	defer func() { ExportWorkspaceEnv(cfg.Workspace) }()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Save defaults BEFORE merging env secrets so the on-disk file
			// never contains plaintext API keys from the environment. A
			// read-only workspace is not fatal, but the user should know the
			// defaults will not persist.
			if saveErr := cfg.Save(path); saveErr != nil {
				slog.Warn("could not persist default config; it will be regenerated on each start",
					"path", path, "error", saveErr)
			}
			cfg.mergeEnvOverrides()
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Env vars always take precedence over file values at runtime but are
	// never written back to disk.
	cfg.mergeEnvOverrides()
	return cfg, nil
}

// FindProviderForModel returns a copy of the provider that supports the given model.
// Returns a value (not a pointer) so callers are insulated from slice reallocations
// caused by later upsertProvider calls.
func (c *Config) FindProviderForModel(model string) *ProviderConfig {
	for i := range c.Providers {
		for _, m := range c.Providers[i].Models {
			if m == model {
				cp := c.Providers[i]
				return &cp
			}
		}
	}
	// Fallback: match by "provider/model" prefix.
	for i := range c.Providers {
		if strings.HasPrefix(model, c.Providers[i].Name+"/") {
			cp := c.Providers[i]
			return &cp
		}
	}
	// Fallback: model name matches provider name directly.
	for i := range c.Providers {
		if strings.EqualFold(model, c.Providers[i].Name) {
			cp := c.Providers[i]
			return &cp
		}
	}
	return nil
}

// ResolveEmbeddingConfig returns the embedding config with its endpoint
// resolved through the configured providers.
//
// The embedding section may leave api_key/base_url empty while the endpoint
// credentials live in a [[providers]] entry (TOML-only, or env-injected). The
// provider is matched by name first, then by the embedding model appearing in
// its model list. Matching by provider *type* is deliberately not attempted:
// several providers share type "openai" (a local LM Studio server, a hosted
// gateway), so a type-only match could silently redirect the embedder to the
// wrong endpoint. Explicit values in the embedding section always win; the
// caller (internal/memory.SetupEmbedder) keeps the OPENAI_API_KEY environment
// fallback for when no provider matched either.
//
// The derived values are kept in unexported fields so they are never persisted
// by Save — see EmbeddingConfig.
func (c *Config) ResolveEmbeddingConfig() EmbeddingConfig {
	e := c.Embedding
	if e.APIKey != "" && e.BaseURL != "" {
		return e
	}
	if p := c.matchEmbeddingProvider(e); p != nil {
		if e.APIKey == "" {
			e.resolvedAPIKey = p.APIKey
		}
		if e.BaseURL == "" {
			e.resolvedBaseURL = p.BaseURL
		}
	}
	return e
}

// matchEmbeddingProvider finds the provider entry that serves the embedding
// endpoint: an exact (case-insensitive) name match first, then the provider
// whose model list contains the configured embedding model. Returns nil when
// neither matches.
func (c *Config) matchEmbeddingProvider(e EmbeddingConfig) *ProviderConfig {
	if e.Provider != "" {
		for i := range c.Providers {
			if strings.EqualFold(c.Providers[i].Name, e.Provider) {
				p := c.Providers[i]
				return &p
			}
		}
	}
	if e.Model != "" {
		for i := range c.Providers {
			for _, m := range c.Providers[i].Models {
				if m == e.Model {
					p := c.Providers[i]
					return &p
				}
			}
		}
	}
	return nil
}

// mergeEnvOverrides applies all environment-variable overrides to the config.
// Env vars take precedence over TOML values at runtime but are never persisted
// to disk. Sections: providers, agent, security, memory, voice, webhook,
// context, learning, heartbeat, channels, search.
func (c *Config) mergeEnvOverrides() {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		c.upsertProvider(ProviderConfig{Name: "openai", Type: "openai", APIKey: key, BaseURL: DefaultOpenAIBaseURL, Models: []string{"gpt-4o", "gpt-4o-mini"}})
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		c.upsertProvider(ProviderConfig{Name: "anthropic", Type: "anthropic", APIKey: key, BaseURL: "https://api.anthropic.com", Models: []string{"claude-sonnet-4-6", "claude-haiku-4-5"}})
	}
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		c.upsertProvider(ProviderConfig{Name: "deepseek", Type: "openai", APIKey: key, BaseURL: "https://api.deepseek.com/v1", Models: []string{"deepseek-chat", "deepseek-reasoner"}})
	}
	if key := os.Getenv("MNEME_CUSTOM_API_KEY"); key != "" {
		baseURL := os.Getenv("MNEME_CUSTOM_BASE_URL")
		if baseURL == "" {
			baseURL = DefaultOpenAIBaseURL
		}
		c.upsertProvider(ProviderConfig{Name: "custom", Type: "openai", APIKey: key, BaseURL: baseURL})
	}

	// knownProviderFields lists the field suffixes recognized in
	// MNEME_PROVIDER_<name>_<field> env vars. Order matters: match
	// longest suffix first so "api_key" is tried before "key".
	knownProviderFields := []string{"api_key", "base_url", "models", "type"}

	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "MNEME_PROVIDER_") {
			continue
		}
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimPrefix(parts[0], "MNEME_PROVIDER_")
		val := parts[1]

		// Match against known field suffixes (longest first) to handle
		// multi-underscore names like "api_key" and "base_url".
		var name, field string
		for _, f := range knownProviderFields {
			suffix := "_" + f
			if strings.HasSuffix(key, suffix) {
				name = key[:len(key)-len(suffix)]
				field = f
				break
			}
		}
		if name == "" {
			continue
		}
		name = strings.ReplaceAll(name, "_", "-")

		found := false
		for i, p := range c.Providers {
			if p.Name == name {
				found = true
				switch field {
				case "type":
					c.Providers[i].Type = val
				case "api_key":
					c.Providers[i].APIKey = val
				case "base_url":
					c.Providers[i].BaseURL = val
				case "models":
					c.Providers[i].Models = strings.Split(val, ",")
				}
				break
			}
		}
		if !found {
			p := ProviderConfig{Name: name}
			switch field {
			case "type":
				p.Type = val
			case "api_key":
				p.APIKey = val
			case "base_url":
				p.BaseURL = val
			case "models":
				p.Models = strings.Split(val, ",")
			}
			c.Providers = append(c.Providers, p)
		}
	}

	if v := os.Getenv("MNEME_AGENT_DEFAULT_MODEL"); v != "" {
		c.Agent.DefaultModel = v
	}
	if v := os.Getenv("MNEME_MAX_OUTPUT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Agent.MaxOutputTokens = n
		}
	}
	if v := os.Getenv("MNEME_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.Agent.Temperature = f
		}
	}
	if v := os.Getenv("MNEME_SECURITY_TIER"); v != "" {
		c.Security.Tier = v
	}
	if v := os.Getenv("MNEME_MEMORY_CHUNK_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Memory.MaxChunkSize = n
		}
	}
	if v := os.Getenv("MNEME_MEMORY_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Memory.RetentionDays = n
		}
	}
	if v := os.Getenv("MNEME_STT_PROVIDER"); v != "" {
		c.Voice.STTProvider = v
	}
	if v := os.Getenv("MNEME_STT_MODEL"); v != "" {
		c.Voice.STTModel = v
	}
	if v := os.Getenv("MNEME_TTS_PROVIDER"); v != "" {
		c.Voice.TTSProvider = v
	}
	if v := os.Getenv("MNEME_TTS_MODEL"); v != "" {
		c.Voice.TTSModel = v
	}
	if v := os.Getenv("MNEME_VOICE_STT_API_KEY"); v != "" {
		c.Voice.STTAPIKey = v
	}
	if v := os.Getenv("MNEME_VOICE_STT_ENDPOINT"); v != "" {
		c.Voice.STTEndpoint = v
	}
	if v := os.Getenv("MNEME_VOICE_TTS_API_KEY"); v != "" {
		c.Voice.TTSAPIKey = v
	}
	if v := os.Getenv("MNEME_VOICE_TTS_ENDPOINT"); v != "" {
		c.Voice.TTSEndpoint = v
	}
	if v := os.Getenv("MNEME_WEBHOOK_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Webhook.Port = n
		}
	}
	if v := os.Getenv("MNEME_WEBHOOK_SECRET"); v != "" {
		c.Webhook.Secret = v
	}
	if v := os.Getenv("MNEME_LEARNING_ENABLED"); v != "" {
		c.Learning.Enabled = v == "1" || v == "true"
	}
	if v := os.Getenv("MNEME_HEARTBEAT_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Cron.HeartbeatIntervalSecs = n
		}
	}
	// ── Embeddings ──
	if v := os.Getenv("MNEME_EMBEDDING_PROVIDER"); v != "" {
		c.Embedding.Provider = v
	}
	if v := os.Getenv("MNEME_EMBEDDING_MODEL"); v != "" {
		c.Embedding.Model = v
	}
	if v := os.Getenv("MNEME_EMBEDDING_BASE_URL"); v != "" {
		c.Embedding.BaseURL = v
	}
	if v := os.Getenv("MNEME_EMBEDDING_API_KEY"); v != "" {
		c.Embedding.APIKey = v
	}

	// ── Search ──
	// Brave: MNEME_SEARCH_BRAVE_KEY preferred; BRAVE_API_KEY as back-compat alias.
	if v := os.Getenv("MNEME_SEARCH_BRAVE_KEY"); v != "" {
		c.Search.BraveAPIKey = v
	} else if v := os.Getenv("BRAVE_API_KEY"); v != "" {
		c.Search.BraveAPIKey = v
	}
	// Tavily: MNEME_SEARCH_TAVILY_KEY preferred; TAVILY_API_KEY as back-compat alias.
	if v := os.Getenv("MNEME_SEARCH_TAVILY_KEY"); v != "" {
		c.Search.TavilyAPIKey = v
	} else if v := os.Getenv("TAVILY_API_KEY"); v != "" {
		c.Search.TavilyAPIKey = v
	}
	// SearXNG
	if v := os.Getenv("SEARXNG_URL"); v != "" {
		c.Search.SearxNGURL = v
	}

	// ── Embedding endpoint ──
	// Resolve last, after providers and MNEME_EMBEDDING_* env overrides are in
	// place, so a TOML-only provider api_key/base_url becomes visible to the
	// embedder. Explicit embedding fields are never overwritten.
	c.Embedding = c.ResolveEmbeddingConfig()
}

func (c *Config) upsertProvider(p ProviderConfig) {
	for i, existing := range c.Providers {
		if existing.Name == p.Name {
			if len(p.Models) > 0 {
				c.Providers[i].Models = p.Models
			}
			if p.APIKey != "" {
				c.Providers[i].APIKey = p.APIKey
			}
			if p.BaseURL != "" {
				c.Providers[i].BaseURL = p.BaseURL
			}
			if p.Type != "" {
				c.Providers[i].Type = p.Type
			}
			return
		}
	}
	c.Providers = append(c.Providers, p)
}

func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var buf strings.Builder
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(c); err != nil {
		return err
	}

	// Write to temp file, fsync, then atomic rename to prevent corruption on crash.
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(buf.String())); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// EffectiveModel returns the model name to use, resolving from config defaults.
func (c *Config) EffectiveModel() string {
	if c.Agent.DefaultModel != "" {
		return c.Agent.DefaultModel
	}
	for _, p := range c.Providers {
		if len(p.Models) > 0 {
			return p.Models[0]
		}
	}
	return ""
}

// Model-route kinds. These are the keys accepted by ModelForRoute and by
// agent definitions' `model` field, so the literal must not drift between the
// call sites that consume a route and the roster that names one.
const (
	RouteCoding    = "coding"    // main agent turn; named by the coder agent's `model` field
	RouteReasoning = "reasoning" // learning reflection, subconscious evaluator, triage
	RouteSummary   = "summary"   // memory archivist, entity extraction, smart walk
	RouteVision    = "vision"    // screenshot analysis
)

// ModelForRoute returns the model configured for a task kind, falling back to
// the effective default model.
//
// A route is consumed in two ways: auxiliary LLM calls read it directly
// (reasoning → learning reflection, subconscious evaluator, webhook triage;
// summary → memory archivist, entity extraction, smart walk; vision →
// screenshot analysis), and agent definitions name a route through their
// `model` field, which eino resolves via CollectRouteModels (coding → coder,
// reasoning → researcher, summary → summarizer).
func (c *Config) ModelForRoute(kind string) string {
	if m, ok := c.Agent.ModelRoutes[kind]; ok && m != "" {
		return m
	}
	return c.EffectiveModel()
}

// BuildSecurityPolicy constructs a SecurityPolicy from config, injecting the
// projects directory as a default readwrite trusted root. The caller wires
// this into the live policy and tool sandbox.
func (c *Config) BuildSecurityPolicy() *security.SecurityPolicy {
	projectsDir := c.ProjectsDir()
	actionDir := c.ResolveActionDir()

	trustedRoots := make([]security.TrustedRoot, 0, len(c.Security.TrustedRoots)+1)
	for _, tr := range c.Security.TrustedRoots {
		access := tr.Access
		if access == "" {
			access = "read"
		}
		trustedRoots = append(trustedRoots, security.TrustedRoot{
			Path:   tr.Path,
			Access: access,
		})
	}
	if projectsDir != "" {
		trustedRoots = append(trustedRoots, security.TrustedRoot{
			Path:   projectsDir,
			Access: "readwrite",
		})
	}

	return &security.SecurityPolicy{
		// An explicit workspace_only is honored on any tier; the readonly tier
		// implies it. Previously the field was read only through the tier
		// comparison, so setting it under supervised/full did nothing.
		WorkspaceOnly:  c.Security.WorkspaceOnly || c.Security.Tier == string(security.TierReadOnly),
		WorkspaceRoot:  c.Workspace,
		ActionDir:      actionDir,
		TrustedRoots:   trustedRoots,
		ForbiddenPaths: c.Security.ForbiddenPaths,
	}
}
