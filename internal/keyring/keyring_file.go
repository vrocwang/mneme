package keyring

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security"
)

// fileStoreMagic prefixes encrypted values so they are unambiguously
// distinguishable from legacy plaintext values written by older versions. A
// plaintext secret of any length (including one that happens to be longer than
// the ChaCha20-Poly1305 nonce) is never mistaken for ciphertext.
const fileStoreMagic = "mneme-fs-v1:"

// FileStore stores secrets in per-key files under a directory. Values are
// encrypted at rest with ChaCha20-Poly1305 using a master key loaded from the
// workspace's encryption.key (security.LoadOrCreateKey). This is the fallback
// when no OS keychain is available; the "local_encrypted" consent label is now
// accurate rather than a misnomer for plaintext storage.
type FileStore struct {
	dir string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) path(service, key string) string {
	return filepath.Join(s.dir, service+"_"+key+".enc")
}

// masterKey loads (or creates) the encryption key for this store. s.dir is the
// workspace's secrets directory, so filepath.Dir(s.dir) is the workspace root
// that LoadOrCreateKey expects.
func (s *FileStore) masterKey() (*[keyLen]byte, error) {
	raw, err := security.LoadOrCreateKey(filepath.Dir(s.dir))
	if err != nil {
		return nil, err
	}
	var key [keyLen]byte
	copy(key[:], raw)
	return &key, nil
}

func (s *FileStore) Get(service, key string) (string, error) {
	blob, err := os.ReadFile(s.path(service, key))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}

	// Encrypted values carry the magic prefix. Anything else is a legacy
	// plaintext value from an older version and is migrated transparently.
	if strings.HasPrefix(string(blob), fileStoreMagic) {
		mk, err := s.masterKey()
		if err != nil {
			return "", err
		}
		plaintext, err := chacha20Decrypt(mk, blob[len(fileStoreMagic):])
		if err != nil {
			return "", err
		}
		return string(plaintext), nil
	}

	// Legacy plaintext: re-encrypt it now (best-effort); the read succeeds
	// regardless of whether migration fails.
	legacy := string(blob)
	_ = s.Set(service, key, legacy)
	return legacy, nil
}

func (s *FileStore) Set(service, key, value string) error {
	mk, err := s.masterKey()
	if err != nil {
		return err
	}
	blob, err := chacha20Encrypt(mk, []byte(value))
	if err != nil {
		return err
	}
	// Prefix with the magic marker so reads can distinguish encrypted values
	// from legacy plaintext unambiguously.
	blob = append([]byte(fileStoreMagic), blob...)
	return os.WriteFile(s.path(service, key), blob, 0600)
}

func (s *FileStore) Delete(service, key string) error {
	err := os.Remove(s.path(service, key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// secretsDir returns the path to the secrets directory, derived from the workspace.
func secretsDir() string {
	workspace := config.WorkspaceDir()
	dir := filepath.Join(workspace, "secrets")
	os.MkdirAll(dir, 0700)
	return dir
}

// defaultStore returns the best available keyring for the current platform.
func defaultStore() Store {
	fileFallback := NewFileStore(secretsDir())
	return platformKeyring(fileFallback)
}
