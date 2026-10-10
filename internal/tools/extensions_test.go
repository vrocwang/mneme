package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindExtensionDirsCoversInstalledLayout guards the fix for a UI-installed
// extension being invisible to the boot-time loader: ExtensionDiscovery
// installs to <root>/<category>/<name> while bundled extensions live at
// <root>/<name>. A flat scan found only the latter, so an installed extension
// loaded for the session and silently vanished after a restart.
func TestFindExtensionDirsCoversInstalledLayout(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, ExtensionsDirName)
	mk := func(rel string) string {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ExtensionManifestFile), []byte(`{"name":"x","version":"1","category":"tools"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	bundled := mk("doctor")
	installed := mk(filepath.Join("tools", "lsp"))

	// Directories without a manifest, and hidden ones, must not be reported.
	if err := os.MkdirAll(filepath.Join(root, "agents", "pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", ExtensionManifestFile), []byte(`{"name":"hidden","version":"1","category":"tools"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := FindExtensionDirs(root)
	if len(got) != 2 {
		t.Fatalf("FindExtensionDirs = %v, want exactly the 2 manifest dirs", got)
	}
	// Lexical order: doctor before tools/lsp.
	if got[0] != bundled || got[1] != installed {
		t.Fatalf("FindExtensionDirs = %v, want [%s %s]", got, bundled, installed)
	}

	// The UI-facing list must agree with the loader's traversal: it uses the
	// same walk and must therefore see the same two extensions.
	list, err := NewExtensionDiscovery(parent).Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("Discover returned %d extensions, want 2 (the loader and the UI must agree)", len(list))
	}
	if list[0].InstallPath != bundled || list[1].InstallPath != installed {
		t.Fatalf("Discover paths = [%s %s], want [%s %s]",
			list[0].InstallPath, list[1].InstallPath, bundled, installed)
	}
}
