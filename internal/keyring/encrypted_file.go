package keyring

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// EncryptedFileBackend stores all secrets in a single ChaCha20-Poly1305
// encrypted JSON file on disk, keyed by an app-scoped master key. The
// master key is stored in the OS keychain and loaded once at startup.
//
// This design reduces OS keychain access to exactly ONE call per process
// lifetime, avoiding the N-prompt problem where dev-signed macOS builds
// block on each individual keychain entry.
type EncryptedFileBackend struct {
	mu        sync.RWMutex
	filePath  string
	masterKey *[keyLen]byte
	// In-memory cache of all secrets to avoid re-decrypting the file on
	// every read.
	cache map[string]string // service_key → value
	dirty bool
}

const secretsFileName = "secrets.enc"

// NewEncryptedFileBackend creates a new EncryptedFileBackend.
// The secrets file is stored at dir/secrets.enc.
func NewEncryptedFileBackend(dir string, masterKey *[keyLen]byte) (*EncryptedFileBackend, error) {
	if masterKey == nil {
		return nil, fmt.Errorf("encrypted_file: master key is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("encrypted_file: create dir %s: %w", dir, err)
	}
	b := &EncryptedFileBackend{
		filePath:  filepath.Join(dir, secretsFileName),
		masterKey: masterKey,
		cache:     make(map[string]string),
	}
	// Load existing secrets on startup.
	if err := b.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("encrypted_file: load: %w", err)
	}
	return b, nil
}

// Get retrieves a secret by service and key.
func (b *EncryptedFileBackend) Get(service, key string) (string, error) {
	b.mu.RLock()
	v, ok := b.cache[cacheKey(service, key)]
	b.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set stores a secret. The encrypted file is rewritten atomically.
func (b *EncryptedFileBackend) Set(service, key, value string) error {
	b.mu.Lock()
	b.cache[cacheKey(service, key)] = value
	b.dirty = true
	b.mu.Unlock()
	return b.save()
}

// Delete removes a secret. The encrypted file is rewritten atomically.
func (b *EncryptedFileBackend) Delete(service, key string) error {
	b.mu.Lock()
	delete(b.cache, cacheKey(service, key))
	b.dirty = true
	b.mu.Unlock()
	return b.save()
}

// ── Persistence ───────────────────────────────────────────────────────

func (b *EncryptedFileBackend) load() error {
	data, err := os.ReadFile(b.filePath)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		b.cache = make(map[string]string)
		return nil
	}

	plaintext, err := chacha20Decrypt(b.masterKey, data)
	if err != nil {
		return fmt.Errorf("encrypted_file: decrypt: %w", err)
	}

	var secrets map[string]string
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		return fmt.Errorf("encrypted_file: unmarshal: %w", err)
	}

	b.cache = secrets
	if b.cache == nil {
		b.cache = make(map[string]string)
	}
	return nil
}

func (b *EncryptedFileBackend) save() error {
	b.mu.RLock()
	secrets := make(map[string]string, len(b.cache))
	for k, v := range b.cache {
		secrets[k] = v
	}
	b.mu.RUnlock()

	plaintext, err := json.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("encrypted_file: marshal: %w", err)
	}

	encrypted, err := chacha20Encrypt(b.masterKey, plaintext)
	if err != nil {
		return fmt.Errorf("encrypted_file: encrypt: %w", err)
	}

	// Atomic write: write to temp file, then rename.
	tmpPath := b.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, encrypted, 0600); err != nil {
		return fmt.Errorf("encrypted_file: write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, b.filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("encrypted_file: rename: %w", err)
	}

	b.mu.Lock()
	b.dirty = false
	b.mu.Unlock()
	return nil
}

// ── Health ─────────────────────────────────────────────────────────────

// IsMasterKeyAvailable returns true if the master key has been loaded.
func (b *EncryptedFileBackend) IsMasterKeyAvailable() bool {
	return b.masterKey != nil
}

// FilePath returns the path to the encrypted secrets file.
func (b *EncryptedFileBackend) FilePath() string {
	return b.filePath
}

// ── Helpers ───────────────────────────────────────────────────────────

func cacheKey(service, key string) string {
	return service + "\x00" + key
}
