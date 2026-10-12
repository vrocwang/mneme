package capability

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/simon/mneme/internal/tools"
)

// An extension whose value is its running process opts out of lazy activation
// with `eager: true`, even when a valid cache entry exists.
func TestEagerManifestForcesStartDespiteCache(t *testing.T) {
	restore := launchExtensionFn
	startRestore := tools.StartExtensionProcess
	t.Cleanup(func() {
		launchExtensionFn = restore
		tools.StartExtensionProcess = startRestore
	})

	var started atomic.Int32
	launchExtensionFn = func(_ *CapabilityRegistry, _ string, mf *tools.ExtensionManifest, _ ExtensionConfigLoader, _ *slog.Logger) (*tools.ProtoProcess, error) {
		started.Add(1)
		return nil, nil
	}
	// A lazy path would use the spawn seam; failing loudly makes an accidental
	// lazy start visible instead of silent.
	tools.StartExtensionProcess = func(_ context.Context, _ string, _ *slog.Logger) (*tools.ProtoProcess, error) {
		t.Error("an eager extension must not be declared lazily")
		return nil, nil
	}

	root := t.TempDir()
	cacheDir := t.TempDir()
	for _, name := range []string{"poller", "chatty"} {
		writeExtension(t, root, name, name, name == "poller")
	}

	// Pre-seed a valid cache entry for BOTH, so the only difference is `eager`.
	for _, name := range []string{"poller", "chatty"} {
		dir := filepath.Join(root, name)
		key, ok := tools.ExtensionCacheKey(dir, filepath.Join(dir, "manifest.json"), filepath.Join(dir, name))
		if !ok {
			t.Fatalf("no cache key for %s", name)
		}
		rec := &tools.CachedExtension{
			Key:      key,
			Manifest: tools.ProtoManifest{Name: name, Version: "1.0.0"},
		}
		if err := tools.SaveExtensionCache(cacheDir, rec); err != nil {
			t.Fatal(err)
		}
	}

	reg := NewCapabilityRegistry()
	discoverExtensions(reg, []string{root}, nil, nil,
		ExtensionLoadOptions{Lazy: true, IdleTimeout: 0, CacheDir: cacheDir},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if got := started.Load(); got != 1 {
		t.Fatalf("eager launches = %d, want exactly 1 (only the `eager: true` extension)", got)
	}
	sets := reg.ListSets()
	var eagerLazy, chattyLazy bool
	for _, s := range sets {
		switch s.Name {
		case "poller":
			eagerLazy = s.Lazy
		case "chatty":
			chattyLazy = s.Lazy
		}
	}
	if eagerLazy {
		t.Error("the `eager: true` extension was declared lazily")
	}
	if !chattyLazy {
		t.Error("the extension without `eager` should have been declared lazily from its warm cache")
	}
}

// writeExtension lays out <root>/<name>/{manifest.json,bin} with an optional
// eager flag. The binary only has to exist for the cache key.
func writeExtension(t *testing.T, root, name, binary string, eager bool) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mf := map[string]interface{}{"name": name, "version": "1.0.0", "category": "tools", "binary": binary}
	if eager {
		mf["eager"] = true
	}
	raw, _ := json.MarshalIndent(mf, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, binary), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
