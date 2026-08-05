package config

// ── Config sections for previously hardcoded values ──────────────────────

// AgentLimits holds operational limits for the agent loop.
type AgentLimits struct {
	MaxToolRounds      int `toml:"max_tool_rounds"      json:"max_tool_rounds"`
	DefaultToolTimeout int `toml:"default_tool_timeout" json:"default_tool_timeout"`
	MaxHistoryMessages int `toml:"max_history_messages" json:"max_history_messages"`
}

func defaultAgentLimits() AgentLimits {
	return AgentLimits{MaxToolRounds: 10, DefaultToolTimeout: 120, MaxHistoryMessages: 100}
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

func defaultRetrievalWeights() RetrievalWeightsConfig {
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
	MaxHardRejects     int `toml:"max_hard_rejects"      json:"max_hard_rejects"`
}

func defaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{MaxRepeatFailures: 3, MaxNoProgressFails: 6, MaxHardRejects: 2}
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
	Mode            string `toml:"mode" json:"mode"` // none, read_only, read_write, sandboxed
	BackendOverride string `toml:"backend_override" json:"backend_override,omitempty"`
}

func defaultSandboxConfig() SandboxConfig {
	return SandboxConfig{Mode: "read_only"}
}

// ActionDirConfig controls the agent's tool sandbox root.
type ActionDirConfig struct {
	Path     string `toml:"path" json:"path"`
	Override string `toml:"override" json:"override,omitempty"`
}

// ObservabilityConfig controls telemetry and error reporting.
type ObservabilityConfig struct {
	SentryDSN      string `toml:"sentry_dsn" json:"sentry_dsn"`
	TracingEnabled bool   `toml:"tracing_enabled" json:"tracing_enabled"`
	LogLevel       string `toml:"log_level" json:"log_level"`
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

// DashboardConfig controls the dashboard display.
type DashboardConfig struct {
	Enabled         bool `toml:"enabled" json:"enabled"`
	RefreshInterval int  `toml:"refresh_interval_secs" json:"refresh_interval_secs"`
}

func defaultDashboardConfig() DashboardConfig {
	return DashboardConfig{Enabled: true, RefreshInterval: 30}
}

// UpdateConfig controls automatic update checking.
type UpdateConfig struct {
	CheckIntervalSecs int    `toml:"check_interval_secs" json:"check_interval_secs"`
	Channel           string `toml:"channel" json:"channel"`
}

func defaultUpdateConfig() UpdateConfig {
	return UpdateConfig{CheckIntervalSecs: 86400, Channel: "stable"}
}

// MeetConfig controls meeting assistant behaviour.
type MeetConfig struct {
	AutoJoin   bool   `toml:"auto_join" json:"auto_join"`
	ListenOnly bool   `toml:"listen_only" json:"listen_only"`
	WakePhrase string `toml:"wake_phrase" json:"wake_phrase"`
	MascotID   string `toml:"mascot_id" json:"mascot_id"`
}

func defaultMeetConfig() MeetConfig {
	return MeetConfig{AutoJoin: false, ListenOnly: true}
}

// ScreenIntelligenceConfig controls screen capture settings.
type ScreenIntelligenceConfig struct {
	Enabled             bool `toml:"enabled" json:"enabled"`
	CaptureIntervalSecs int  `toml:"capture_interval_secs" json:"capture_interval_secs"`
}

func defaultScreenIntelligenceConfig() ScreenIntelligenceConfig {
	return ScreenIntelligenceConfig{Enabled: false, CaptureIntervalSecs: 5}
}

// RuntimeConfig controls managed runtime (Node/Python) settings.
type RuntimeConfig struct {
	AutoInstall bool                `toml:"auto_install" json:"auto_install"`
	Version     string              `toml:"version" json:"version,omitempty"`
	Node        NodeRuntimeConfig   `toml:"node" json:"node"`
	Python      PythonRuntimeConfig `toml:"python" json:"python"`
}

// NodeRuntimeConfig controls managed Node.js runtime settings.
type NodeRuntimeConfig struct {
	AutoInstall bool   `toml:"auto_install" json:"auto_install"`
	Version     string `toml:"version" json:"version,omitempty"`
	InstallDir  string `toml:"install_dir" json:"install_dir,omitempty"`
}

// PythonRuntimeConfig controls managed Python runtime settings.
type PythonRuntimeConfig struct {
	AutoInstall bool   `toml:"auto_install" json:"auto_install"`
	Version     string `toml:"version" json:"version,omitempty"`
	InstallDir  string `toml:"install_dir" json:"install_dir,omitempty"`
	VenvPath    string `toml:"venv_path" json:"venv_path,omitempty"`
}

// TaskSourcesConfig controls proactive task ingestion from external tools.
type TaskSourcesConfig struct {
	Enabled     bool `toml:"enabled" json:"enabled"`
	PollMinutes int  `toml:"poll_minutes" json:"poll_minutes"`
}

func defaultTaskSourcesConfig() TaskSourcesConfig {
	return TaskSourcesConfig{Enabled: false, PollMinutes: 30}
}

// InferenceHTTPConfig controls the local inference HTTP server.
type InferenceHTTPConfig struct {
	Enabled bool   `toml:"enabled" json:"enabled"`
	Port    int    `toml:"port" json:"port"`
	Bind    string `toml:"bind" json:"bind"`
}

func defaultInferenceHTTPConfig() InferenceHTTPConfig {
	return InferenceHTTPConfig{Enabled: true, Port: 8080, Bind: "127.0.0.1"}
}
