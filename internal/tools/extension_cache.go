package tools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Handshake cache.
//
// Every extension is a separate process whose boot handshake (extension.describe
// plus extension.list_tools/extension.list_agents) costs a process spawn. A
// typical session uses a handful of the installed extensions, so the host
// records each successful handshake here and, on the next boot, declares the
// extension from the record without starting anything. The process starts on
// first use (see LazyProcess in extension_lazy.go).
//
// The cache key is a hash of the extension's own bytes (manifest plus the
// binary or, for interpreted extensions, every file in its directory), so an
// extension that changed on disk is always a miss and takes the eager path.
// The cache *directory* is chosen by the caller (internal/boot derives it from
// the workspace data dir): this package only reads and writes the record it is
// handed, so internal/tools stays free of any workspace-layout knowledge.

// extensionCacheSchemaVersion guards the on-disk record format. Bump it when
// CachedExtension changes shape; a record written by a different schema version
// is a miss, never a parse error that reaches boot.
const extensionCacheSchemaVersion = 1

const (
	// extensionCacheMaxFiles and extensionCacheMaxBytes bound how much of an
	// extension directory the key will hash. Interpreted extensions (a script
	// plus its helper modules) are hashed file-by-file; past these bounds the
	// key is refused and the caller falls back to starting the extension
	// eagerly instead of paying an unbounded read at boot.
	extensionCacheMaxFiles = 64
	extensionCacheMaxBytes = 64 << 20

	// extensionCacheFileSuffix is the file name suffix of a cache record.
	extensionCacheFileSuffix = ".json"

	// extensionCacheKeyDomain separates this hash from any other use of sha256
	// over the same bytes.
	extensionCacheKeyDomain = "mneme-extension-handshake-cache-v1"
)

// CachedExtension is one extension's handshake result, replayable at the next
// boot without starting its process: the manifest plus the tool and agent
// definitions extension.list_tools/extension.list_agents returned.
//
// Tools and Agents use the wire types (protoToolDef, AgentDef) deliberately:
// they are the exact JSON the extension sent, so a replayed declaration cannot
// drift from a live one.
type CachedExtension struct {
	SchemaVersion int            `json:"schema_version"`
	Key           string         `json:"key"`
	Manifest      ProtoManifest  `json:"manifest"`
	Tools         []protoToolDef `json:"tools"`
	Agents        []AgentDef     `json:"agents"`
	CachedAt      time.Time      `json:"cached_at"`
}

// ExtensionCacheKey derives the cache key for one extension.
//
// dir is the extension directory, manifestPath its manifest.json, and binaryArg
// the resolved binary path or interpreter command from the manifest (the value
// capability.resolveExtensionBinary produces). The key hashes the manifest
// bytes plus either the binary's bytes or — for an interpreter invocation
// ("python3 script.py") or a script with a shebang, where behaviour may span
// several files — every regular file in the extension directory.
//
// The boolean is false when a safe key cannot be derived: the manifest or the
// binary is unreadable, or the directory exceeds the scan bounds above. The
// caller must then fall back to an eager start.
func ExtensionCacheKey(dir, manifestPath, binaryArg string) (string, bool) {
	dir = strings.TrimSpace(dir)
	manifestPath = strings.TrimSpace(manifestPath)
	if dir == "" || manifestPath == "" {
		return "", false
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", false
	}

	h := sha256.New()
	hashCacheField(h, []byte(extensionCacheKeyDomain))
	hashCacheField(h, manifestBytes)

	if isInterpretedBinary(binaryArg) {
		if !hashExtensionDir(h, dir) {
			return "", false
		}
	} else {
		binaryBytes, err := os.ReadFile(strings.TrimSpace(binaryArg))
		if err != nil {
			return "", false
		}
		hashCacheField(h, binaryBytes)
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// isInterpretedBinary reports whether the command is an interpreter invocation
// ("python3 script.py") or a script with a shebang — an extension whose
// behaviour may live in more files than the one the manifest names.
func isInterpretedBinary(command string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	if len(parts) > 1 {
		return true
	}
	return fileHasShebang(parts[0])
}

// fileHasShebang reports whether path starts with "#!". A native executable
// cannot start with those bytes.
func fileHasShebang(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false
	}
	return head[0] == '#' && head[1] == '!'
}

// hashCacheField writes a length-prefixed field, so "ab"+"c" and "a"+"bc"
// cannot produce the same digest.
func hashCacheField(h io.Writer, b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	_, _ = h.Write(n[:])
	_, _ = h.Write(b)
}

// hashExtensionDir hashes every regular file in dir, in the lexical order
// filepath.WalkDir guarantees, including each file's relative path so a rename
// changes the key. It returns false (refusing the key) when the directory
// cannot be walked or exceeds the scan bounds. Symlinks are skipped: their
// targets are outside the extension's own bytes.
func hashExtensionDir(h io.Writer, dir string) bool {
	var files int
	var total int64
	ok := true
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			ok = false
			return fs.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		files++
		if files > extensionCacheMaxFiles {
			ok = false
			return fs.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			ok = false
			return fs.SkipAll
		}
		total += info.Size()
		if total > extensionCacheMaxBytes {
			ok = false
			return fs.SkipAll
		}
		data, err := os.ReadFile(path)
		if err != nil {
			ok = false
			return fs.SkipAll
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		hashCacheField(h, []byte(filepath.ToSlash(rel)))
		hashCacheField(h, data)
		return nil
	})
	if walkErr != nil {
		return false
	}
	return ok
}

// ExtensionCachePath returns the record path for key inside cacheDir.
func ExtensionCachePath(cacheDir, key string) string {
	return filepath.Join(cacheDir, key+extensionCacheFileSuffix)
}

// LoadExtensionCache reads the record for key from cacheDir.
//
// A miss — unreadable file, corrupt JSON, a record from another schema version,
// or a record whose key does not match — is reported as (nil, false). Callers
// treat that as "start eagerly"; the cache must never be able to break boot.
func LoadExtensionCache(cacheDir, key string) (*CachedExtension, bool) {
	if cacheDir == "" || key == "" {
		return nil, false
	}
	data, err := os.ReadFile(ExtensionCachePath(cacheDir, key))
	if err != nil {
		return nil, false
	}
	var rec CachedExtension
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, false
	}
	if rec.SchemaVersion != extensionCacheSchemaVersion {
		return nil, false
	}
	if rec.Key != key || rec.Manifest.Name == "" {
		return nil, false
	}
	return &rec, true
}

// SaveExtensionCache writes rec atomically (temp file in the same directory,
// then rename) with mode 0o644, stamping the current schema version. A partial
// or interrupted write can therefore never be read back as a valid record: the
// rename publishes either the whole record or nothing.
func SaveExtensionCache(cacheDir string, rec *CachedExtension) error {
	if strings.TrimSpace(cacheDir) == "" {
		return errors.New("extension cache dir is empty")
	}
	if rec == nil || rec.Key == "" {
		return errors.New("extension cache record has no key")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create extension cache dir: %w", err)
	}
	rec.SchemaVersion = extensionCacheSchemaVersion
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal extension cache: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(cacheDir, "extension-cache-*.tmp")
	if err != nil {
		return fmt.Errorf("create extension cache temp file: %w", err)
	}
	tmpName := tmp.Name()
	discard := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o644); err != nil {
		discard()
		return fmt.Errorf("chmod extension cache temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		discard()
		return fmt.Errorf("write extension cache temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		discard()
		return fmt.Errorf("sync extension cache temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close extension cache temp file: %w", err)
	}
	if err := os.Rename(tmpName, ExtensionCachePath(cacheDir, rec.Key)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("publish extension cache record: %w", err)
	}
	return nil
}
