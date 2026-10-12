package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCacheFixture creates an extension directory with a manifest and a binary,
// and returns (dir, manifestPath, binaryPath).
func writeCacheFixture(t *testing.T, binaryContent string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "ext-bin")
	if err := os.WriteFile(binaryPath, []byte(binaryContent), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	manifestPath := filepath.Join(dir, ExtensionManifestFile)
	manifest := `{"name":"cache-ext","version":"1.0.0","category":"tools","binary":"ext-bin"}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir, manifestPath, binaryPath
}

// A record written by one boot must be readable by the next, with the whole
// tool/agent surface intact — that is what lets the next boot declare the
// extension without starting it.
func TestExtensionCacheRoundTrip(t *testing.T) {
	cacheDir := t.TempDir()
	key := "0123456789abcdef"
	rec := &CachedExtension{
		Key:      key,
		Manifest: ProtoManifest{Name: "cache-ext", Version: "1.0.0", ProtocolMin: 1},
		Tools: []protoToolDef{
			{Name: "cache_ext_echo", Description: "echo", Permission: "read-only", Parameters: map[string]interface{}{"type": "object"}},
		},
		Agents:   []AgentDef{{ID: "cache-agent", Name: "Cached Agent"}},
		CachedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := SaveExtensionCache(cacheDir, rec); err != nil {
		t.Fatalf("SaveExtensionCache: %v", err)
	}

	got, ok := LoadExtensionCache(cacheDir, key)
	if !ok {
		t.Fatal("LoadExtensionCache missed a record it just wrote")
	}
	if got.SchemaVersion != extensionCacheSchemaVersion {
		t.Errorf("schema version = %d, want %d", got.SchemaVersion, extensionCacheSchemaVersion)
	}
	if got.Manifest.Name != "cache-ext" || got.Manifest.Version != "1.0.0" {
		t.Errorf("manifest = %+v, want cache-ext@1.0.0", got.Manifest)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "cache_ext_echo" || got.Tools[0].Permission != "read-only" {
		t.Errorf("tools = %+v, want the single echo definition", got.Tools)
	}
	if len(got.Agents) != 1 || got.Agents[0].ID != "cache-agent" {
		t.Errorf("agents = %+v, want the single cached agent", got.Agents)
	}
	if !got.CachedAt.Equal(rec.CachedAt) {
		t.Errorf("cached_at = %v, want %v", got.CachedAt, rec.CachedAt)
	}
}

// Changing the bytes of the binary the manifest points at must change the key:
// a stale cache entry must never be replayed against a different build.
func TestExtensionCacheKeyChangesWithBinary(t *testing.T) {
	dir, manifestPath, binaryPath := writeCacheFixture(t, "first build")

	key1, ok := ExtensionCacheKey(dir, manifestPath, binaryPath)
	if !ok {
		t.Fatal("ExtensionCacheKey refused a readable fixture")
	}
	if err := os.WriteFile(binaryPath, []byte("second build"), 0o755); err != nil {
		t.Fatalf("rewrite binary: %v", err)
	}
	key2, ok := ExtensionCacheKey(dir, manifestPath, binaryPath)
	if !ok {
		t.Fatal("ExtensionCacheKey refused the rewritten fixture")
	}
	if key1 == key2 {
		t.Fatalf("key did not change after the binary changed (%s)", key1)
	}
	// And the old key is a miss against the new directory state.
	if _, hit := LoadExtensionCache(t.TempDir(), key1); hit {
		t.Fatal("a key with no record must miss")
	}
}

// Changing the manifest also changes the key: the version and tool declarations
// live there.
func TestExtensionCacheKeyChangesWithManifest(t *testing.T) {
	dir, manifestPath, binaryPath := writeCacheFixture(t, "binary")

	key1, _ := ExtensionCacheKey(dir, manifestPath, binaryPath)
	if err := os.WriteFile(manifestPath, []byte(`{"name":"cache-ext","version":"2.0.0","category":"tools","binary":"ext-bin"}`), 0o644); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
	key2, ok := ExtensionCacheKey(dir, manifestPath, binaryPath)
	if !ok {
		t.Fatal("ExtensionCacheKey refused the updated manifest")
	}
	if key1 == key2 {
		t.Fatal("key did not change after the manifest changed")
	}
}

// An interpreted extension ("sh script.sh" or a shebang script) may span several
// files, so any file in its directory participates in the key.
func TestExtensionCacheKeyHashesWholeDirectoryForScripts(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "ext.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	manifestPath := filepath.Join(dir, ExtensionManifestFile)
	if err := os.WriteFile(manifestPath, []byte(`{"name":"script-ext","version":"1.0.0","category":"tools","binary":"sh ext.sh"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	helper := filepath.Join(dir, "helper.py")
	if err := os.WriteFile(helper, []byte("VALUE = 1\n"), 0o644); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	key1, ok := ExtensionCacheKey(dir, manifestPath, "sh "+script)
	if !ok {
		t.Fatal("ExtensionCacheKey refused an interpreter command")
	}
	// A change in a file the manifest never names must still invalidate the key.
	if err := os.WriteFile(helper, []byte("VALUE = 2\n"), 0o644); err != nil {
		t.Fatalf("rewrite helper: %v", err)
	}
	key2, ok := ExtensionCacheKey(dir, manifestPath, "sh "+script)
	if !ok {
		t.Fatal("ExtensionCacheKey refused the updated directory")
	}
	if key1 == key2 {
		t.Fatal("key ignored a helper file change in an interpreted extension")
	}

	// A single-path script with a shebang is detected without the "sh " prefix.
	key3, ok := ExtensionCacheKey(dir, manifestPath, script)
	if !ok {
		t.Fatal("ExtensionCacheKey refused a shebang script passed as a single path")
	}
	if key3 != key2 {
		t.Error("a shebang script must hash like the interpreter invocation of the same directory")
	}
}

// The scan bound is a fallback, not an error: past it the caller starts eagerly.
func TestExtensionCacheKeyRefusesOversizedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ext.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	manifestPath := filepath.Join(dir, ExtensionManifestFile)
	if err := os.WriteFile(manifestPath, []byte(`{"name":"big-ext","version":"1.0.0","category":"tools","binary":"sh ext.sh"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	for i := 0; i <= extensionCacheMaxFiles; i++ {
		name := filepath.Join(dir, fmt.Sprintf("extra-%03d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("write extra file: %v", err)
		}
	}
	if _, ok := ExtensionCacheKey(dir, manifestPath, "sh "+filepath.Join(dir, "ext.sh")); ok {
		t.Fatalf("directory with more than %d files must be refused", extensionCacheMaxFiles)
	}
}

// A key that cannot be derived (unreadable manifest or binary) is a refusal, so
// the caller falls back to an eager start.
func TestExtensionCacheKeyRefusesUnreadableInputs(t *testing.T) {
	dir, manifestPath, binaryPath := writeCacheFixture(t, "binary")
	if _, ok := ExtensionCacheKey(dir, filepath.Join(dir, "absent.json"), binaryPath); ok {
		t.Error("missing manifest must be refused")
	}
	if _, ok := ExtensionCacheKey(dir, manifestPath, filepath.Join(dir, "absent-bin")); ok {
		t.Error("missing binary must be refused")
	}
	if _, ok := ExtensionCacheKey("", manifestPath, binaryPath); ok {
		t.Error("empty dir must be refused")
	}
}

// Every miss mode must be a miss, never a panic or a bogus record.
func TestLoadExtensionCacheMisses(t *testing.T) {
	cacheDir := t.TempDir()
	key := "abc123"

	if _, ok := LoadExtensionCache(cacheDir, key); ok {
		t.Error("absent record must miss")
	}
	if _, ok := LoadExtensionCache(cacheDir, ""); ok {
		t.Error("empty key must miss")
	}
	if _, ok := LoadExtensionCache("", key); ok {
		t.Error("empty cache dir must miss")
	}

	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(ExtensionCachePath(cacheDir, key), []byte(content), 0o644); err != nil {
			t.Fatalf("write raw record: %v", err)
		}
	}

	// Corrupt JSON.
	write("{not json")
	if _, ok := LoadExtensionCache(cacheDir, key); ok {
		t.Error("corrupt JSON must miss")
	}

	// Wrong schema version, everything else valid.
	good := &CachedExtension{Key: key, Manifest: ProtoManifest{Name: "x"}}
	write(`{"schema_version":99,"key":"abc123","manifest":{"name":"x"}}`)
	if _, ok := LoadExtensionCache(cacheDir, key); ok {
		t.Error("schema version mismatch must miss")
	}

	// Key mismatch: a record filed under the wrong name is not this extension's.
	good.Key = "other-key"
	data, err := json.Marshal(good)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	write(string(data))
	if _, ok := LoadExtensionCache(cacheDir, key); ok {
		t.Error("record whose key does not match must miss")
	}

	// Nameless manifest: nothing usable to declare.
	good.Key = key
	good.Manifest = ProtoManifest{}
	data, err = json.Marshal(good)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	write(string(data))
	if _, ok := LoadExtensionCache(cacheDir, key); ok {
		t.Error("record without a manifest name must miss")
	}
}

// The write must publish the whole record in one rename: no temp file survives,
// the mode is 0o644, and an existing record is replaced atomically.
func TestSaveExtensionCacheIsAtomicAndReplaceable(t *testing.T) {
	cacheDir := t.TempDir()
	key := "atomic-key"

	for i := 0; i < 3; i++ {
		rec := &CachedExtension{Key: key, Manifest: ProtoManifest{Name: "x", Version: string(rune('a' + i))}}
		if err := SaveExtensionCache(cacheDir, rec); err != nil {
			t.Fatalf("SaveExtensionCache #%d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("cache dir holds %v, want exactly the published record (no temp files)", names)
	}
	if got := entries[0].Name(); got != key+extensionCacheFileSuffix {
		t.Fatalf("published file = %q, want %q", got, key+extensionCacheFileSuffix)
	}
	info, err := os.Stat(ExtensionCachePath(cacheDir, key))
	if err != nil {
		t.Fatalf("stat record: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("record mode = %o, want 644", perm)
	}

	got, ok := LoadExtensionCache(cacheDir, key)
	if !ok {
		t.Fatal("replaced record is unreadable")
	}
	if got.Manifest.Version != "c" {
		t.Errorf("version = %q, want the last write (c)", got.Manifest.Version)
	}
}

// An unusable record must be rejected before the file is touched.
func TestSaveExtensionCacheRejectsBadInput(t *testing.T) {
	if err := SaveExtensionCache("", &CachedExtension{Key: "k"}); err == nil {
		t.Error("empty cache dir must be an error")
	}
	cacheDir := t.TempDir()
	if err := SaveExtensionCache(cacheDir, nil); err == nil {
		t.Error("nil record must be an error")
	}
	if err := SaveExtensionCache(cacheDir, &CachedExtension{}); err == nil {
		t.Error("record without a key must be an error")
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("rejected writes left %d files behind", len(entries))
	}
}
