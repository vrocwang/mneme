package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetWorkspace_FullMigration(t *testing.T) {
	// ── Setup: create old workspace with realistic files ─────────────────
	oldRoot := t.TempDir()

	write := func(rel string, data []byte) {
		path := filepath.Join(oldRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("config.toml", []byte(`workspace = "`+oldRoot+`"
schema_version = 0
[agent]
  default_model = "llama3"`))
	write("secrets/credential.enc", []byte("encrypted-abc123"))
	write("secrets/sub/nested.enc", []byte("nested-secret"))
	write("memory/conversations/chat.json", []byte(`{"messages": [{"role": "user", "content": "hello"}]}`))
	write("data/mneme.db", []byte("sqlite-binary-data"))
	write("logs/app.log", []byte("log line 1\nlog line 2\n"))
	write(".hidden-cache", []byte("cache-data"))
	write("screenshots/screen1.png", []byte("\x89PNG\x00\x00\x00\rIHDR"))

	// ── Create ConfigRPC with the old workspace ─────────────────────────
	cfg := DefaultConfig()
	cfg.Workspace = oldRoot
	configPath := filepath.Join(oldRoot, "config.toml")

	rpc := NewConfigRPC(cfg, configPath)
	rpc.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// ── Execute: migrate to new workspace ───────────────────────────────
	newRoot := filepath.Join(t.TempDir(), "new-workspace")
	if err := rpc.SetWorkspace(newRoot); err != nil {
		t.Fatalf("SetWorkspace failed: %v", err)
	}

	// ── Verify: old workspace is gone ─────────────────────────────────
	if _, err := os.Stat(oldRoot); !os.IsNotExist(err) {
		t.Errorf("old workspace should be removed, but it still exists at %s", oldRoot)
	}

	// ── Verify: all files migrated with correct contents ───────────────
	verify := func(rel, expected string) {
		got, err := os.ReadFile(filepath.Join(newRoot, rel))
		if err != nil {
			t.Errorf("missing file %s: %v", rel, err)
			return
		}
		if string(got) != expected {
			t.Errorf("file %s: content mismatch\n  got:  %q\n  want: %q", rel, string(got), expected)
		}
	}

	verify("secrets/credential.enc", "encrypted-abc123")
	verify("secrets/sub/nested.enc", "nested-secret")
	verify("memory/conversations/chat.json", `{"messages": [{"role": "user", "content": "hello"}]}`)
	verify("data/mneme.db", "sqlite-binary-data")
	verify("logs/app.log", "log line 1\nlog line 2\n")
	verify(".hidden-cache", "cache-data")
	verify("screenshots/screen1.png", "\x89PNG\x00\x00\x00\rIHDR")

	// ── Verify: config.toml was saved with updated workspace ───────────
	configData, err := os.ReadFile(filepath.Join(newRoot, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not found in new workspace: %v", err)
	}
	if !strings.Contains(string(configData), `workspace = "`+newRoot+`"`) {
		t.Errorf("config.toml should contain updated workspace path:\n%s", string(configData))
	}

	// ── Verify: cfg fields are updated ──────────────────────────────────
	if rpc.GetWorkspace() != newRoot {
		t.Errorf("GetWorkspace: got %s, want %s", rpc.GetWorkspace(), newRoot)
	}
}

func TestSetWorkspace_MigrationFailsPreservesOldWorkspace(t *testing.T) {
	oldRoot := t.TempDir()

	// Create a file in old workspace.
	configPath := filepath.Join(oldRoot, "config.toml")
	configContent := `workspace = "` + oldRoot + `"
schema_version = 0
[agent]
  default_model = "llama3"`
	os.MkdirAll(filepath.Dir(configPath), 0755)
	os.WriteFile(configPath, []byte(configContent), 0644)

	// Create a secrets directory with a file.
	secretsDir := filepath.Join(oldRoot, "secrets")
	os.MkdirAll(secretsDir, 0755)
	os.WriteFile(filepath.Join(secretsDir, "key.enc"), []byte("my-secret"), 0644)

	cfg := DefaultConfig()
	cfg.Workspace = oldRoot
	rpc := NewConfigRPC(cfg, configPath)
	rpc.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Make newRoot invalid for writing by using a path that can't be created.
	// Create a file where a directory needs to be created — this will cause
	// MkdirAll to fail.
	newRoot := filepath.Join(t.TempDir(), "blocker")
	// Create "blocker" as a file so MkdirAll("blocker/subdir") fails.
	os.WriteFile(newRoot, []byte("block"), 0644)

	err := rpc.SetWorkspace(filepath.Join(newRoot, "subdir"))
	if err == nil {
		t.Fatal("expected SetWorkspace to fail when new workspace cannot be created")
	}
	t.Logf("SetWorkspace correctly failed: %v", err)

	// Verify old workspace is still intact.
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("old config.toml should still exist: %v", err)
	}
	secretData, err := os.ReadFile(filepath.Join(secretsDir, "key.enc"))
	if err != nil || string(secretData) != "my-secret" {
		t.Errorf("old secrets should still be intact: err=%v, data=%s", err, string(secretData))
	}
}

func TestSetWorkspace_SameWorkspaceRejected(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	os.WriteFile(configPath, []byte(`workspace = "`+root+`"\n`), 0644)

	cfg := DefaultConfig()
	cfg.Workspace = root
	rpc := NewConfigRPC(cfg, configPath)

	// Same path (absolute) should be rejected.
	if err := rpc.SetWorkspace(root); err == nil {
		t.Error("expected error when setting same workspace")
	}

	// Tilde expansion: create a workspace under the home directory.
	home, _ := os.UserHomeDir()
	homeWorkspace := filepath.Join(home, "test-workspace-same")
	os.MkdirAll(homeWorkspace, 0755)
	defer os.RemoveAll(homeWorkspace)

	cfg2 := DefaultConfig()
	cfg2.Workspace = homeWorkspace
	rpc2 := NewConfigRPC(cfg2, filepath.Join(homeWorkspace, "config.toml"))
	os.WriteFile(filepath.Join(homeWorkspace, "config.toml"), []byte(`workspace = "`+homeWorkspace+`"`), 0644)

	tildePath := "~/" + strings.TrimPrefix(homeWorkspace, home+"/")
	if err := rpc2.SetWorkspace(tildePath); err == nil {
		t.Errorf("expected error when setting same workspace via tilde %q", tildePath)
	}
}
