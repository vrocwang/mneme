package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Extension subprocesses learn the workspace from MNEME_HOME (extsdk
// .WorkspaceDir). The variable must therefore track the *active* workspace:
// a `workspace` override in the file, and any later SetWorkspace, have to be
// reflected, and headless modes (which never set the variable themselves) must
// not leave extensions falling back to the binary's directory.
func TestLoadExportsWorkspaceEnv(t *testing.T) {
	prev, hadPrev := os.LookupEnv("MNEME_HOME")
	t.Cleanup(func() {
		if hadPrev {
			os.Setenv("MNEME_HOME", prev)
		} else {
			os.Unsetenv("MNEME_HOME")
		}
	})

	root := t.TempDir()
	other := t.TempDir()

	// Pretend the process started with a different root.
	os.Setenv("MNEME_HOME", other)

	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("workspace = \""+root+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Workspace != root {
		t.Fatalf("cfg.Workspace = %q, want %q", cfg.Workspace, root)
	}
	if got := os.Getenv("MNEME_HOME"); got != root {
		t.Fatalf("MNEME_HOME = %q, want the active workspace %q (children would write elsewhere)", got, root)
	}

	// ExportWorkspaceEnv is idempotent and rejects an empty value.
	ExportWorkspaceEnv("")
	if os.Getenv("MNEME_HOME") != root {
		t.Fatal("an empty workspace must not clear MNEME_HOME")
	}

	// A workspace change (ConfigRPC.SetWorkspace path) re-exports it.
	next := t.TempDir()
	ExportWorkspaceEnv(next)
	if got := os.Getenv("MNEME_HOME"); got != next {
		t.Fatalf("MNEME_HOME = %q, want %q after a workspace change", got, next)
	}
}
