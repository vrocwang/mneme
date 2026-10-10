package config

import pkgtools "github.com/simon/mneme/pkg/tools"

// ── Config sections for previously hardcoded values ──────────────────────

// AgentLimits holds operational limits for the agent loop.
type AgentLimits struct {
	MaxToolRounds      int `toml:"max_tool_rounds"      json:"max_tool_rounds"`
	DefaultToolTimeout int `toml:"default_tool_timeout" json:"default_tool_timeout"`
}

func defaultAgentLimits() AgentLimits {
	return AgentLimits{MaxToolRounds: pkgtools.DefaultMaxIterations, DefaultToolTimeout: pkgtools.DefaultTimeoutSecs}
}

// SecurityCommands holds custom command classification lists.
type SecurityCommands struct {
	BlockHighRisk         bool     `toml:"block_high_risk"          json:"block_high_risk"`
	RequireMediumApproval bool     `toml:"require_medium_approval"  json:"require_medium_approval"`
	ExtraReadOnly         []string `toml:"extra_read_only"          json:"extra_read_only"`
	ExtraNetwork          []string `toml:"extra_network"            json:"extra_network"`
	ExtraDestructive      []string `toml:"extra_destructive"        json:"extra_destructive"`
	ExtraInstall          []string `toml:"extra_install"            json:"extra_install"`
	ExtraExecutors        []string `toml:"extra_executors"          json:"extra_executors"`
	ExtraDangerousEnv     []string `toml:"extra_dangerous_env"      json:"extra_dangerous_env"`
	ExtraAllowedCommands  []string `toml:"extra_allowed_commands"   json:"extra_allowed_commands"`
}

func defaultSecurityCommands() SecurityCommands {
	return SecurityCommands{BlockHighRisk: true, RequireMediumApproval: false}
}

// MemoryPipelineConfig holds memory pipeline tuning parameters.
type MemoryPipelineConfig struct {
	WorkerCount       int     `toml:"worker_count"        json:"worker_count"`
	TreeBucketSize    int     `toml:"tree_bucket_size"    json:"tree_bucket_size"`
	ArchiveMsgLimit   int     `toml:"archive_msg_limit"   json:"archive_msg_limit"`
	FreshnessHalfLife float64 `toml:"freshness_half_life" json:"freshness_half_life"`
}

func defaultMemoryPipelineConfig() MemoryPipelineConfig {
	return MemoryPipelineConfig{WorkerCount: 2, TreeBucketSize: 10, ArchiveMsgLimit: 200, FreshnessHalfLife: 168}
}

// RetrievalWeightsConfig holds signal weights for multi-strategy memory retrieval.
type RetrievalWeightsConfig struct {
	Profile  string  `toml:"profile"  json:"profile"` // "balanced", "semantic", "lexical", "graph_first", "" = custom
	FTS5     float64 `toml:"fts5"     json:"fts5"`
	Vector   float64 `toml:"vector"   json:"vector"`
	Keyword  float64 `toml:"keyword"  json:"keyword"`
	Tree     float64 `toml:"tree"     json:"tree"`
	Graph    float64 `toml:"graph"    json:"graph"`
	Episodic float64 `toml:"episodic" json:"episodic"`
}

// DefaultRetrievalWeights returns the authoritative default signal weights.
// internal/memory derives its fallback weights from this, so the two cannot
// drift apart (they previously duplicated the same numbers with a "keep in
// sync" comment).
func DefaultRetrievalWeights() RetrievalWeightsConfig {
	return RetrievalWeightsConfig{FTS5: 0.30, Vector: 0.25, Keyword: 0.08, Tree: 0.0, Graph: 0.22, Episodic: 0.15}
}

// ToolsShellConfig holds shell tool configuration.
type ToolsShellConfig struct {
	MaxOutputBytes int      `toml:"max_output_bytes" json:"max_output_bytes"`
	SafeEnvVars    []string `toml:"safe_env_vars"    json:"safe_env_vars"`
}

func defaultToolsShellConfig() ToolsShellConfig {
	return ToolsShellConfig{
		MaxOutputBytes: 1 << 20, // 1MB
		SafeEnvVars:    nil,     // nil means use hardcoded defaults
	}
}

// CircuitBreakerConfig holds circuit breaker thresholds.
type CircuitBreakerConfig struct {
	MaxRepeatFailures  int `toml:"max_repeat_failures"   json:"max_repeat_failures"`
	MaxNoProgressFails int `toml:"max_no_progress_fails" json:"max_no_progress_fails"`
}

func defaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{MaxRepeatFailures: 3, MaxNoProgressFails: 6}
}

// CostConfig holds cost tracking configuration.
type CostConfig struct {
	BudgetCents int `toml:"budget_cents" json:"budget_cents"`
}

func defaultCostConfig() CostConfig {
	return CostConfig{BudgetCents: 10000}
}

// ── Config accessors ────────────────────────────────────────────────────

// GetAgentLimits returns the agent limits config, merging in defaults for zero values.
func (c *Config) GetAgentLimits() AgentLimits { return c.Agent.Limits }

// GetSecurityCommands returns the security commands config.
func (c *Config) GetSecurityCommands() SecurityCommands { return c.Security.Commands }

// GetMemoryPipelineConfig returns the memory pipeline config.
func (c *Config) GetMemoryPipelineConfig() MemoryPipelineConfig { return c.Memory.Pipeline }

// GetRetrievalWeights returns the retrieval signal weights.
func (c *Config) GetRetrievalWeights() RetrievalWeightsConfig { return c.Memory.RetrievalWeights }

// GetToolsShellConfig returns the shell tool config.
func (c *Config) GetToolsShellConfig() ToolsShellConfig { return c.Tools.Shell }

// GetCircuitBreakerConfig returns the circuit breaker config.
func (c *Config) GetCircuitBreakerConfig() CircuitBreakerConfig { return c.CircuitBreaker }

// GetCostConfig returns the cost tracking config.
func (c *Config) GetCostConfig() CostConfig { return c.Cost }

// ── Autonomy, Sandbox, Observability, and other config sections ───────

// AutonomyConfig controls the agent's autonomy level and security boundaries.
// Deprecated: parsed for config-file compatibility but not wired to any
// consumer (security tier + approval gate govern autonomy at runtime).
type AutonomyConfig struct {
	Level                        string             `toml:"level" json:"level"`
	WorkspaceOnly                bool               `toml:"workspace_only" json:"workspace_only"`
	AllowedCommands              []string           `toml:"allowed_commands" json:"allowed_commands"`
	ForbiddenPaths               []string           `toml:"forbidden_paths" json:"forbidden_paths"`
	MaxActionsPerHour            int                `toml:"max_actions_per_hour" json:"max_actions_per_hour"`
	MaxCostPerDayCents           int                `toml:"max_cost_per_day_cents" json:"max_cost_per_day_cents"`
	RequireApprovalForMediumRisk bool               `toml:"require_approval_for_medium_risk" json:"require_approval_for_medium_risk"`
	BlockHighRiskCommands        bool               `toml:"block_high_risk_commands" json:"block_high_risk_commands"`
	AutoApprove                  []string           `toml:"auto_approve" json:"auto_approve"`
	TrustedRoots                 []TrustedRootEntry `toml:"trusted_roots" json:"trusted_roots"`
	AllowToolInstall             bool               `toml:"allow_tool_install" json:"allow_tool_install"`
	RequireTaskPlanApproval      bool               `toml:"require_task_plan_approval" json:"require_task_plan_approval"`
}

func defaultAutonomyConfig() AutonomyConfig {
	return AutonomyConfig{Level: "supervised", WorkspaceOnly: true}
}

// SandboxConfig controls command execution sandboxing.
type SandboxConfig struct {
	// Mode selects the sandbox policy: "none" (no sandboxing), "read_only" or
	// "read_write" (use the best available backend, which exposes the workspace
	// as the one writable root), or "sandboxed" (same, but refuse to run at all
	// when no backend is available). "disabled" is a legacy alias for "none".
	// An unknown value is logged and falls back to the detected sandbox.
	Mode            string `toml:"mode" json:"mode"`
	BackendOverride string `toml:"backend_override" json:"backend_override,omitempty"`
}

func defaultSandboxConfig() SandboxConfig {
	return SandboxConfig{Mode: "read_only"}
}

// ActionDirConfig controls the agent's tool sandbox root.
type ActionDirConfig struct {
	Override string `toml:"override" json:"override,omitempty"`
}

// ObservabilityConfig controls application logging. Remote error reporting
// (Sentry) and tracing (OpenTelemetry) are not implemented.
type ObservabilityConfig struct {
	LogLevel string `toml:"log_level" json:"log_level"`
}

func defaultObservabilityConfig() ObservabilityConfig {
	return ObservabilityConfig{LogLevel: "info"}
}

// ProxyConfig controls HTTP proxy settings.
type ProxyConfig struct {
	HTTPProxy  string `toml:"http_proxy" json:"http_proxy"`
	HTTPSProxy string `toml:"https_proxy" json:"https_proxy"`
	NoProxy    string `toml:"no_proxy" json:"no_proxy"`
}

// ScreenIntelligenceConfig controls screen capture settings.
type ScreenIntelligenceConfig struct {
	Enabled             bool `toml:"enabled" json:"enabled"`
	CaptureIntervalSecs int  `toml:"capture_interval_secs" json:"capture_interval_secs"`
}

// DefaultScreenCaptureIntervalSecs is the documented screen-capture cadence in
// seconds. It is the single source for the config default.
//
// Remaining divergence (cannot be fixed from this package): the desktop loop
// in internal/desktop/screen_intel.go substitutes a hardcoded 30s fallback
// when the interval is zero. That package exports no constant for it and
// internal/config must not import internal/desktop, so the two values still
// disagree for a zero-valued interval; the config default below is non-zero,
// which is what the runtime actually uses.
const DefaultScreenCaptureIntervalSecs = 5

func defaultScreenIntelligenceConfig() ScreenIntelligenceConfig {
	return ScreenIntelligenceConfig{Enabled: false, CaptureIntervalSecs: DefaultScreenCaptureIntervalSecs}
}

// InferenceHTTPConfig controls the local inference HTTP server.
type InferenceHTTPConfig struct {
	Enabled bool   `toml:"enabled" json:"enabled"`
	Port    int    `toml:"port" json:"port"`
	Bind    string `toml:"bind" json:"bind"`
}

func defaultInferenceHTTPConfig() InferenceHTTPConfig {
	return InferenceHTTPConfig{Enabled: true, Port: DefaultInferenceHTTPPort, Bind: DefaultLoopbackBind}
}
