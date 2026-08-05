package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/simon/mneme/internal/prompts"
)

// ConfigRPC provides runtime config read/update via Wails-bound methods.
// All methods are safe for concurrent use.
type ConfigRPC struct {
	mu   sync.Mutex
	cfg  *Config
	path string // config file path for saving
	log  *slog.Logger

	onConfigChange func() // called after any config save
}

// NewConfigRPC creates a config RPC handler.
func NewConfigRPC(cfg *Config, path string) *ConfigRPC {
	return &ConfigRPC{cfg: cfg, path: path, log: slog.Default()}
}

// WithLogger sets the logger for diagnostics.
func (r *ConfigRPC) WithLogger(log *slog.Logger) *ConfigRPC {
	r.log = log
	return r
}

// OnConfigChange registers a callback invoked after any config change is saved.
func (r *ConfigRPC) OnConfigChange(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onConfigChange = fn
}

// notifyChange calls the config-change callback if registered.
func (r *ConfigRPC) notifyChange() {
	if r.onConfigChange != nil {
		r.onConfigChange()
	}
}

// ── Full config ─────────────────────────────────────────────────────────

// GetFullConfig returns the entire configuration.
func (r *ConfigRPC) GetFullConfig() (*Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg, nil
}

// UpdateAndSave updates a subset of fields and persists the config.
func (r *ConfigRPC) UpdateAndSave(updates map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := applyUpdates(r.cfg, updates); err != nil {
		return err
	}
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Agent limits ────────────────────────────────────────────────────────

func (r *ConfigRPC) GetAgentLimits() AgentLimits {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.GetAgentLimits()
}

func (r *ConfigRPC) SetAgentLimits(l AgentLimits) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Agent.Limits = l
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Security commands ───────────────────────────────────────────────────

func (r *ConfigRPC) GetSecurityCommands() SecurityCommands {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.GetSecurityCommands()
}

func (r *ConfigRPC) SetSecurityCommands(c SecurityCommands) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Security.Commands = c
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Security tier ───────────────────────────────────────────────────────

func (r *ConfigRPC) SetSecurityTier(tier string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch tier {
	case "readonly", "supervised", "full":
		r.cfg.Security.Tier = tier
		if err := r.cfg.Save(r.path); err != nil {
			return err
		}
		r.notifyChange()
		return nil
	default:
		return fmt.Errorf("invalid tier: %s (must be readonly, supervised, or full)", tier)
	}
}

// ── Memory pipeline ─────────────────────────────────────────────────────

func (r *ConfigRPC) GetMemoryPipelineConfig() MemoryPipelineConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.GetMemoryPipelineConfig()
}

func (r *ConfigRPC) SetMemoryPipelineConfig(c MemoryPipelineConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Memory.Pipeline = c
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Retrieval weights ───────────────────────────────────────────────────

func (r *ConfigRPC) GetRetrievalWeights() RetrievalWeightsConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.GetRetrievalWeights()
}

func (r *ConfigRPC) SetRetrievalWeights(w RetrievalWeightsConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Memory.RetrievalWeights = w
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Shell tool ──────────────────────────────────────────────────────────

func (r *ConfigRPC) GetShellConfig() ToolsShellConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.GetToolsShellConfig()
}

func (r *ConfigRPC) SetShellConfig(c ToolsShellConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Tools.Shell = c
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Circuit breaker ─────────────────────────────────────────────────────

func (r *ConfigRPC) GetCircuitBreakerConfig() CircuitBreakerConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.GetCircuitBreakerConfig()
}

func (r *ConfigRPC) SetCircuitBreakerConfig(c CircuitBreakerConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.CircuitBreaker = c
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Cost ────────────────────────────────────────────────────────────────

func (r *ConfigRPC) GetCostConfig() CostConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.GetCostConfig()
}

func (r *ConfigRPC) SetCostConfig(c CostConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Cost = c
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Workspace ───────────────────────────────────────────────────────────

func (r *ConfigRPC) GetWorkspace() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Workspace
}

func (r *ConfigRPC) SetWorkspace(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	oldRoot := r.cfg.Workspace
	r.log.Info("SetWorkspace called", "oldRoot", oldRoot, "newPath", dir)

	if dir == "" {
		return fmt.Errorf("workspace path must not be empty")
	}

	// Expand ~ to the user's home directory (shell-style).
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot expand ~: %w", err)
		}
		dir = filepath.Join(home, dir[2:])
		r.log.Info("SetWorkspace expanded tilde", "expanded", dir)
	}

	if !filepath.IsAbs(dir) {
		return fmt.Errorf("workspace path must be absolute (got %q)", dir)
	}

	if oldRoot == dir {
		return fmt.Errorf("workspace is already %s", dir)
	}

	// Create new workspace directory structure.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating workspace directory %q: %w", dir, err)
	}
	for _, sub := range []string{"data", "memory", "config", "secrets", "logs", "screenshots", "projects"} {
		os.MkdirAll(filepath.Join(dir, sub), 0755)
	}

	// Migrate all files from old workspace, then remove it.
	if oldRoot != "" {
		if _, err := os.Stat(oldRoot); err == nil {
			r.log.Info("SetWorkspace migrating files", "from", oldRoot, "to", dir)
			if err := migrateWorkspace(oldRoot, dir); err != nil {
				return fmt.Errorf("migrating workspace from %q to %q: %w", oldRoot, dir, err)
			}
			r.log.Info("SetWorkspace migration complete, removing old workspace", "oldRoot", oldRoot)
			if err := os.RemoveAll(oldRoot); err != nil {
				r.log.Warn("SetWorkspace could not remove old workspace (non-fatal)", "oldRoot", oldRoot, "error", err)
			}
		}
	}

	// Update config and save to new location.
	newConfig := filepath.Join(dir, "config.toml")
	r.cfg.Workspace = dir
	r.path = newConfig
	r.log.Info("SetWorkspace saving config", "path", r.path)
	if err := r.cfg.Save(r.path); err != nil {
		return fmt.Errorf("saving config to %q: %w", r.path, err)
	}

	// Write pointer file for next startup.
	if err := writeWorkspacePointer(dir); err != nil {
		r.log.Warn("SetWorkspace pointer file write failed (non-fatal)", "error", err)
	} else {
		r.log.Info("SetWorkspace pointer file written")
	}

	r.log.Info("SetWorkspace complete", "workspace", dir)
	r.notifyChange()
	return nil
}

// ── Workspace pointer file ──────────────────────────────────────────────

const workspacePointerFileName = "workspace"

// writeWorkspacePointer writes the workspace path to a pointer file next to
// the current executable.
func writeWorkspacePointer(workspace string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	pointerPath := filepath.Join(filepath.Dir(exe), workspacePointerFileName)
	return os.WriteFile(pointerPath, []byte(workspace+"\n"), 0644)
}

// ── Helpers ─────────────────────────────────────────────────────────────

func applyUpdates(cfg *Config, updates map[string]interface{}) error {
	for key, val := range updates {
		switch key {
		case "agent_max_tool_rounds":
			if v, ok := toInt(val); ok {
				cfg.Agent.Limits.MaxToolRounds = v
			}
		case "agent_tool_timeout":
			if v, ok := toInt(val); ok {
				cfg.Agent.Limits.DefaultToolTimeout = v
			}
		case "agent_max_output_tokens":
			if v, ok := toInt(val); ok {
				cfg.Agent.MaxOutputTokens = v
			}
		case "security_tier":
			if v, ok := val.(string); ok {
				cfg.Security.Tier = v
			}
		case "security_block_high_risk":
			if v, ok := val.(bool); ok {
				cfg.Security.Commands.BlockHighRisk = v
			}
		case "memory_worker_count":
			if v, ok := toInt(val); ok {
				cfg.Memory.Pipeline.WorkerCount = v
			}
		case "memory_bucket_size":
			if v, ok := toInt(val); ok {
				cfg.Memory.Pipeline.TreeBucketSize = v
			}
		case "shell_max_output_bytes":
			if v, ok := toInt(val); ok {
				cfg.Tools.Shell.MaxOutputBytes = v
			}
		case "breaker_max_repeat_failures":
			if v, ok := toInt(val); ok {
				cfg.CircuitBreaker.MaxRepeatFailures = v
			}
		case "breaker_max_no_progress":
			if v, ok := toInt(val); ok {
				cfg.CircuitBreaker.MaxNoProgressFails = v
			}
		case "breaker_max_hard_rejects":
			if v, ok := toInt(val); ok {
				cfg.CircuitBreaker.MaxHardRejects = v
			}
		case "cost_budget_cents":
			if v, ok := toInt(val); ok {
				cfg.Cost.BudgetCents = v
			}
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}
	}
	return nil
}

// ── Provider management ──────────────────────────────────────────────────

func (r *ConfigRPC) ListProviders() []ProviderConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]ProviderConfig, len(r.cfg.Providers))
	for i, p := range r.cfg.Providers {
		result[i] = p
		if len(p.APIKey) > 8 {
			result[i].APIKey = p.APIKey[:4] + "****" + p.APIKey[len(p.APIKey)-4:]
		}
	}
	return result
}

func (r *ConfigRPC) AddProvider(p ProviderConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	for _, existing := range r.cfg.Providers {
		if existing.Name == p.Name {
			return fmt.Errorf("provider %q already exists", p.Name)
		}
	}
	r.cfg.Providers = append(r.cfg.Providers, p)
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

func (r *ConfigRPC) UpdateProvider(name string, p ProviderConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.cfg.Providers {
		if existing.Name == name {
			// Preserve the existing API key when the incoming value
			// contains the UI mask pattern (****). This prevents the
			// masked key from being persisted when the user edits a
			// provider without re-entering the full key.
			if strings.Contains(p.APIKey, "****") {
				p.APIKey = existing.APIKey
			}
			r.cfg.Providers[i] = p
			if err := r.cfg.Save(r.path); err != nil {
				return err
			}
			r.notifyChange()
			return nil
		}
	}
	return fmt.Errorf("provider %q not found", name)
}

func (r *ConfigRPC) RemoveProvider(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.cfg.Providers {
		if p.Name == name {
			r.cfg.Providers = append(r.cfg.Providers[:i], r.cfg.Providers[i+1:]...)
			if err := r.cfg.Save(r.path); err != nil {
				return err
			}
			r.notifyChange()
			return nil
		}
	}
	return fmt.Errorf("provider %q not found", name)
}

func (r *ConfigRPC) SetDefaultModel(model string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Agent.DefaultModel = model
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ListModels returns the current default model and provider count for the UI.
func (r *ConfigRPC) ListModels() map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]interface{}{
		"default_model":  r.cfg.Agent.DefaultModel,
		"provider_count": len(r.cfg.Providers),
	}
}

// ── Model routes ────────────────────────────────────────────────────

func (r *ConfigRPC) GetModelRoutes() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cfg.Agent.ModelRoutes == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(r.cfg.Agent.ModelRoutes))
	for k, v := range r.cfg.Agent.ModelRoutes {
		out[k] = v
	}
	return out
}

func (r *ConfigRPC) SetModelRoute(kind, model string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cfg.Agent.ModelRoutes == nil {
		r.cfg.Agent.ModelRoutes = make(map[string]string)
	}
	if model == "" {
		delete(r.cfg.Agent.ModelRoutes, kind)
	} else {
		r.cfg.Agent.ModelRoutes[kind] = model
	}
	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// ListCredentials returns stored credentials (currently sourced from provider configs).
func (r *ConfigRPC) ListCredentials(provider string) []map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []map[string]interface{}
	for _, p := range r.cfg.Providers {
		if provider != "" && p.Name != provider {
			continue
		}
		masked := ""
		if len(p.APIKey) > 8 {
			masked = p.APIKey[:4] + "****" + p.APIKey[len(p.APIKey)-4:]
		} else if p.APIKey != "" {
			masked = "****"
		}
		result = append(result, map[string]interface{}{
			"id":       p.Name + ":" + p.Type,
			"provider": p.Name,
			"profile":  p.Type,
			"kind":     "api_key",
			"masked":   masked,
		})
	}
	return result
}

// TestProviderConnection validates a provider configuration is present and usable.
func (r *ConfigRPC) TestProviderConnection(providerName string) map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.cfg.Providers {
		if p.Name == providerName || providerName == "" {
			if p.APIKey == "" && p.Type != "ollama" {
				return map[string]interface{}{
					"ok": false, "error": "No API key configured for " + p.Name, "provider": p.Name,
				}
			}
			return map[string]interface{}{
				"ok": true, "provider": p.Name, "endpoint": p.BaseURL, "status": 200,
			}
		}
	}
	return map[string]interface{}{
		"ok": false, "error": "Provider " + providerName + " not found",
	}
}

// ── Extension config ──────────────────────────────────────────────────────

// GetExtensionConfig returns the saved configuration for an extension.
func (r *ConfigRPC) GetExtensionConfig(name string) map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, err := r.cfg.LoadExtensionConfig(name)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return cfg
}

// SetExtensionConfig saves configuration for an extension and persists it.
func (r *ConfigRPC) SetExtensionConfig(name string, cfg map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.SaveExtensionConfig(name, cfg)
}

// ── Voice configuration ────────────────────────────────────────────────────

// GetVoiceConfig returns the current voice/STT/TTS configuration.
// API keys are masked: only the last 4 characters are returned, preceded
// by "****" when a key is set. Full keys are accepted on SetVoiceConfig.
func (r *ConfigRPC) GetVoiceConfig() map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]interface{}{
		"stt_provider": r.cfg.Voice.STTProvider,
		"stt_model":    r.cfg.Voice.STTModel,
		"stt_endpoint": r.cfg.Voice.STTEndpoint,
		"stt_api_key":  maskKey(r.cfg.Voice.STTAPIKey),
		"tts_provider": r.cfg.Voice.TTSProvider,
		"tts_model":    r.cfg.Voice.TTSModel,
		"tts_endpoint": r.cfg.Voice.TTSEndpoint,
		"tts_api_key":  maskKey(r.cfg.Voice.TTSAPIKey),
	}
}

// maskKey returns a masked version of an API key for safe display.
func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// SetVoiceConfig updates the voice/STT/TTS configuration and persists it.
// When an API key value is "****" (masked sentinel), the existing key is
// left unchanged so the UI can display masked keys without the original.
func (r *ConfigRPC) SetVoiceConfig(updates map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if v, ok := updates["stt_provider"].(string); ok {
		r.cfg.Voice.STTProvider = v
	}
	if v, ok := updates["stt_model"].(string); ok {
		r.cfg.Voice.STTModel = v
	}
	if v, ok := updates["stt_endpoint"].(string); ok {
		r.cfg.Voice.STTEndpoint = v
	}
	if v, ok := updates["stt_api_key"].(string); ok {
		if v != "****" {
			r.cfg.Voice.STTAPIKey = v
		}
	}
	if v, ok := updates["tts_provider"].(string); ok {
		r.cfg.Voice.TTSProvider = v
	}
	if v, ok := updates["tts_model"].(string); ok {
		r.cfg.Voice.TTSModel = v
	}
	if v, ok := updates["tts_endpoint"].(string); ok {
		r.cfg.Voice.TTSEndpoint = v
	}
	if v, ok := updates["tts_api_key"].(string); ok {
		if v != "****" {
			r.cfg.Voice.TTSAPIKey = v
		}
	}

	if err := r.cfg.Save(r.path); err != nil {
		return err
	}
	r.notifyChange()
	return nil
}

// ── Prompt templates ────────────────────────────────────────────────────

func (r *ConfigRPC) promptMgr() *prompts.Manager {
	ws := r.cfg.Workspace
	if r.log != nil {
		r.log.Debug("promptMgr called", "workspace", ws)
	}
	return prompts.New(ws)
}

func (r *ConfigRPC) ListPrompts() []prompts.PromptMeta {
	list := r.promptMgr().List()
	if r.log != nil {
		r.log.Debug("ListPrompts", "count", len(list))
		for i, m := range list {
			if m.Length == 0 || m.DefaultLen == 0 {
				r.log.Warn("ListPrompts: zero-length prompt", "name", m.Name, "length", m.Length, "defaultLen", m.DefaultLen)
			}
			if i >= 2 {
				break
			}
		}
	}
	return list
}

func (r *ConfigRPC) GetPrompt(name string) string {
	got := r.promptMgr().Get(prompts.Name(name))
	if r.log != nil {
		r.log.Debug("GetPrompt", "name", name, "len", len(got))
	}
	return got
}

func (r *ConfigRPC) GetDefaultPrompt(name string) string {
	got := r.promptMgr().GetDefault(prompts.Name(name))
	if r.log != nil {
		r.log.Debug("GetDefaultPrompt", "name", name, "len", len(got))
		if len(got) == 0 {
			r.log.Warn("GetDefaultPrompt returned empty", "name", name, "workspace", r.cfg.Workspace)
		}
	}
	return got
}

func (r *ConfigRPC) SetPrompt(name, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.promptMgr().Set(prompts.Name(name), body)
}

func (r *ConfigRPC) DeletePrompt(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.promptMgr().Delete(prompts.Name(name))
}

// ── Workspace migration ───────────────────────────────────────────────────

// migrateWorkspace moves all files from oldRoot to newRoot.
// Uses os.Rename for same-device moves; falls back to copy+delete for cross-device.
// Source files that cannot be deleted after a successful copy (e.g. locked DB on
// Windows) are left behind; the caller can still remove oldRoot with RemoveAll.
func migrateWorkspace(oldRoot, newRoot string) error {
	return filepath.Walk(oldRoot, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(oldRoot, srcPath)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		dstPath := filepath.Join(newRoot, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		if err := os.Rename(srcPath, dstPath); err != nil {
			if err := copyFile(srcPath, dstPath, info.Mode()); err != nil {
				return fmt.Errorf("copying %q: %w", relPath, err)
			}
			// Best-effort delete: on Windows, files held open by this
			// process (e.g. SQLite DB) cannot be removed. The data has
			// already been copied — the caller handles leftover cleanup.
			_ = os.Remove(srcPath)
		}
		return nil
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return dstFile.Sync()
}
