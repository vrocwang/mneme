package capability

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon/mneme/internal/tools"
	ws "github.com/simon/mneme/internal/workspace"
)

// fakeExtensionScript is a minimal JSON-RPC extension process: enough of the
// protocol for the real loader to handshake, list tools, and register the set.
// It exists so the disabled-extension tests observe the actual launch path, not
// a stubbed filter.
const fakeExtensionScript = `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
  case "$method" in
    extension.describe|manifest)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"name":"fake-ext","version":"0.0.1","tools":[],"agent_defs":[],"protocol_min":1}}\n' "$id"
      ;;
    extension.list_tools)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}\n' "$id"
      ;;
  esac
done
`

// writeFakeExtension creates <root>/<name>/manifest.json plus its launcher
// script, so tools.FindExtensionDirs discovers it and the loader really starts
// a subprocess for it.
func writeFakeExtension(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fake.sh"), []byte(fakeExtensionScript), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	manifest := fmt.Sprintf(`{"name":%q,"version":"0.1.0","category":"tools","binary":"sh fake.sh"}`, name)
	if err := os.WriteFile(filepath.Join(dir, tools.ExtensionManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func requireExtensionSet(t *testing.T, reg *CapabilityRegistry, name string, want bool) {
	t.Helper()
	_, ok := reg.GetSet(ExtensionSetID(name))
	if ok != want {
		t.Fatalf("set %q present = %v, want %v", ExtensionSetID(name), ok, want)
	}
}

// TestNewExtensionFilter covers the id-matching rule: the manifest name and the
// capability-set id are both accepted, empty/blank lists mean "enable all", and
// ids that match nothing are inert.
func TestNewExtensionFilter(t *testing.T) {
	if NewExtensionFilter(nil) != nil {
		t.Error("nil list must produce a nil filter (everything enabled)")
	}
	if NewExtensionFilter([]string{"", "  "}) != nil {
		t.Error("blank-only list must produce a nil filter")
	}

	byName := NewExtensionFilter([]string{"todos"})
	if byName == nil {
		t.Fatal("expected non-nil filter")
	}
	if !byName("todos") {
		t.Error("manifest name must match")
	}
	if byName("file-state") {
		t.Error("unrelated name must not match")
	}

	bySetID := NewExtensionFilter([]string{ExtensionSetID("todos")})
	if bySetID == nil || !bySetID("todos") {
		t.Error("set id (" + ExtensionSetID("todos") + ") must match the manifest name")
	}

	unknown := NewExtensionFilter([]string{"not-an-extension"})
	if unknown == nil {
		t.Fatal("a non-empty list must produce a filter")
	}
	if unknown("todos") {
		t.Error("unknown id must not match any extension")
	}
}

// TestDiscoverExtensionsSkipsDisabled proves the end-to-end contract of item 1:
// a manifest whose name is in [bundles] disabled is never launched or
// registered, is skipped before its binary is resolved (so no auto-build), and
// says so at info level.
func TestDiscoverExtensionsSkipsDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake extension uses a POSIX shell script")
	}
	root := t.TempDir()
	writeFakeExtension(t, root, "todos")

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	log, buf := testLogger()

	filter := NewExtensionFilter([]string{"todos"})
	reg.SetExtensionFilter(filter)
	discoverExtensions(reg, []string{root}, nil, filter, ExtensionLoadOptions{}, log)

	requireExtensionSet(t, reg, "todos", false)
	if !strings.Contains(buf.String(), "extension disabled by config") {
		t.Errorf("expected an info line explaining the skip, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "extension loaded") {
		t.Errorf("disabled extension must not be launched, got:\n%s", buf.String())
	}
}

// TestDiscoverExtensionsLoadsWhenEnabled is the control: with the same fixture
// and no disabled entry (or after removing it) the extension is registered, so
// the skip above is caused by the filter and not by a broken fixture.
func TestDiscoverExtensionsLoadsWhenEnabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake extension uses a POSIX shell script")
	}
	root := t.TempDir()
	writeFakeExtension(t, root, "todos")

	t.Run("no filter", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		defer reg.Shutdown()
		log, buf := testLogger()

		discoverExtensions(reg, []string{root}, nil, NewExtensionFilter(nil), ExtensionLoadOptions{}, log)
		requireExtensionSet(t, reg, "todos", true)
		if !strings.Contains(buf.String(), "extension loaded") {
			t.Errorf("expected the extension to load, got:\n%s", buf.String())
		}
	})

	t.Run("unknown id is harmless", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		defer reg.Shutdown()
		log, buf := testLogger()

		filter := NewExtensionFilter([]string{"some-other-extension"})
		discoverExtensions(reg, []string{root}, nil, filter, ExtensionLoadOptions{}, log)
		requireExtensionSet(t, reg, "todos", true)
		if !strings.Contains(buf.String(), "extension loaded") {
			t.Errorf("unknown disabled id must not block loading, got:\n%s", buf.String())
		}
	})

	t.Run("disabled by set id", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		defer reg.Shutdown()
		log, _ := testLogger()

		filter := NewExtensionFilter([]string{ExtensionSetID("todos")})
		discoverExtensions(reg, []string{root}, nil, filter, ExtensionLoadOptions{}, log)
		requireExtensionSet(t, reg, "todos", false)
	})
}

// TestBootstrapSkipsDisabledExtension exercises the public entry point the boot
// layer calls, proving Bootstrap threads the id list into discovery and
// publishes the filter on the registry.
func TestBootstrapSkipsDisabledExtension(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake extension uses a POSIX shell script")
	}
	workspace := t.TempDir()
	writeFakeExtension(t, filepath.Join(workspace, ws.DirExtensions), "todos")

	t.Run("disabled", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		defer reg.Shutdown()
		log, buf := testLogger()

		Bootstrap(reg, workspace, nil, nil, []string{"todos"}, ExtensionLoadOptions{}, log)

		requireExtensionSet(t, reg, "todos", false)
		if !strings.Contains(buf.String(), "extension disabled by config") {
			t.Errorf("expected bootstrap to skip the disabled extension, got:\n%s", buf.String())
		}
	})

	t.Run("enabled again", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		defer reg.Shutdown()
		log, _ := testLogger()

		Bootstrap(reg, workspace, nil, nil, nil, ExtensionLoadOptions{}, log)

		requireExtensionSet(t, reg, "todos", true)
	})
}

// TestLaunchExtensionProcWarnsWhenDisabled covers the install-time contract: a
// just-installed extension must still load (refusing would look like a failed
// install) while logging that config disables it and it will not start again.
func TestLaunchExtensionProcWarnsWhenDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake extension uses a POSIX shell script")
	}
	root := t.TempDir()
	writeFakeExtension(t, root, "file-state")
	script := filepath.Join(root, "file-state", "fake.sh")

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	reg.SetExtensionFilter(NewExtensionFilter([]string{"file-state"}))
	log, buf := testLogger()

	proc, err := launchExtensionProc(reg, "sh "+script, &tools.ExtensionManifest{Name: "file-state"}, nil, log)
	if err != nil {
		t.Fatalf("install-time load of a config-disabled extension failed: %v", err)
	}
	if proc == nil {
		t.Fatal("expected a live extension process")
	}
	requireExtensionSet(t, reg, "file-state", true)
	if !strings.Contains(buf.String(), "disabled by config; loaded for this session but will not run at the next start") {
		t.Errorf("expected the install-time warning, got:\n%s", buf.String())
	}
}
