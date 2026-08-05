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
