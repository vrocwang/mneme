package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_DefaultsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Workspace != defaultWorkspaceDir() {
		t.Errorf("expected default workspace, got %s", cfg.Workspace)
	}
	if cfg.Agent.DefaultModel != "llama3" {
		t.Errorf("expected default model llama3, got %s", cfg.Agent.DefaultModel)
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	os.Setenv("MNEME_AGENT_DEFAULT_MODEL", "claude-sonnet-4-6")
	defer os.Unsetenv("MNEME_AGENT_DEFAULT_MODEL")

	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Agent.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("expected env override, got %s", cfg.Agent.DefaultModel)
	}
}

func TestLoadConfig_FileOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`[agent]
default_model = "ollama/llama3"`)
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, content, 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Agent.DefaultModel != "ollama/llama3" {
		t.Errorf("expected ollama/llama3 from file, got %s", cfg.Agent.DefaultModel)
	}
}

func TestFindProviderForModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = append(cfg.Providers, ProviderConfig{
		Name:    "deepseek",
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: "https://api.deepseek.com/v1",
		Models:  []string{"deepseek-chat", "deepseek-reasoner"},
	})

	p := cfg.FindProviderForModel("deepseek-chat")
	if p == nil {
		t.Fatal("expected to find provider for deepseek-chat")
	}
	if p.Name != "deepseek" {
		t.Errorf("expected deepseek, got %s", p.Name)
	}

	p2 := cfg.FindProviderForModel("nonexistent-model")
	if p2 != nil {
		t.Error("expected nil for unknown model")
	}
}

func TestConfig_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := DefaultConfig()
	cfg.Agent.DefaultModel = "test-model"
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Agent.DefaultModel != "test-model" {
		t.Errorf("round-trip failed: got %s", loaded.Agent.DefaultModel)
	}
}

// TestExtensionsConfigDefaults pins the [extensions] defaults: lazy loading is
// on and an idle process lives ten minutes. The zero value of IdleTimeoutSecs
// means "never reap", so the default must be an explicit number, and a config
// file that omits the section must keep both defaults.
func TestExtensionsConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Extensions.Lazy {
		t.Error("lazy extension loading must default to true")
	}
	if cfg.Extensions.IdleTimeoutSecs != DefaultExtensionIdleTimeoutSecs {
		t.Errorf("default idle timeout = %d, want %d",
			cfg.Extensions.IdleTimeoutSecs, DefaultExtensionIdleTimeoutSecs)
	}
	if DefaultExtensionIdleTimeoutSecs == 0 {
		t.Fatal("the default idle timeout must not be 0 (0 means never reap)")
	}

	// A file without [extensions] keeps the defaults.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[agent]\ndefault_model = \"m\"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Extensions.Lazy || loaded.Extensions.IdleTimeoutSecs != DefaultExtensionIdleTimeoutSecs {
		t.Errorf("missing [extensions] section changed the defaults: %+v", loaded.Extensions)
	}
}

// A [extensions] section overrides both knobs; 0 means never reap.
func TestExtensionsConfigFromTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[extensions]\nlazy = false\nidle_timeout_secs = 0\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Extensions.Lazy {
		t.Error("lazy = false in the file must disable lazy loading")
	}
	if cfg.Extensions.IdleTimeoutSecs != 0 {
		t.Errorf("idle_timeout_secs = 0 must be honoured as never-reap, got %d", cfg.Extensions.IdleTimeoutSecs)
	}
}
