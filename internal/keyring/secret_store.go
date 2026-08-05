package keyring

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
)

// SecretStore provides ChaCha20-Poly1305 encryption for individual config
// fields (API keys, tokens, etc.). Encrypted values use the "enc2:" prefix
// scheme. Legacy "enc:" XOR cipher values are decrypted transparently and
// should be re-encrypted on write.
//
// The master key is loaded once from the OS keychain at startup and cached
// process-wide.
type SecretStore struct {
	masterKey *[keyLen]byte
}

// EncryptedPayload is the wire format for encrypted config values.
const enc2Prefix = "enc2:"

// NewSecretStore creates a SecretStore backed by the given master key.
// If masterKey is nil, encryption is unavailable (decryption-only for
// legacy values).
func NewSecretStore(masterKey *[keyLen]byte) *SecretStore {
	return &SecretStore{masterKey: masterKey}
}

// Encrypt encrypts a plaintext string and returns the "enc2:<base64>" form.
// Returns an error if the master key is not available.
func (s *SecretStore) Encrypt(plaintext string) (string, error) {
	if s.masterKey == nil {
		return "", fmt.Errorf("secret_store: master key not available — cannot encrypt")
	}
	blob, err := chacha20Encrypt(s.masterKey, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("secret_store encrypt: %w", err)
	}
	return enc2Prefix + base64.StdEncoding.EncodeToString(blob), nil
}

// Decrypt decrypts a value that may be an "enc2:" or legacy "enc:" prefix,
// or plaintext. Returns the decrypted value.
func (s *SecretStore) Decrypt(value string) (string, error) {
	if strings.HasPrefix(value, enc2Prefix) {
		return s.decryptEnc2(value)
	}
	if strings.HasPrefix(value, "enc:") {
		return s.decryptLegacy(value)
	}
	// Plaintext — no decryption needed.
	return value, nil
}

// NeedsReEncrypt returns true when the value should be re-encrypted
// (legacy format or plaintext when a master key is available).
func (s *SecretStore) NeedsReEncrypt(value string) bool {
	return s.masterKey != nil && (!strings.HasPrefix(value, enc2Prefix))
}

func (s *SecretStore) decryptEnc2(value string) (string, error) {
	if s.masterKey == nil {
		return "", fmt.Errorf("secret_store: master key not available — cannot decrypt enc2")
	}
	encoded := strings.TrimPrefix(value, enc2Prefix)
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secret_store: invalid enc2 base64: %w", err)
	}
	plaintext, err := chacha20Decrypt(s.masterKey, blob)
	if err != nil {
		return "", fmt.Errorf("secret_store: enc2 decryption failed: %w", err)
	}
	return string(plaintext), nil
}

// decryptLegacy handles the old "enc:" XOR cipher format. This is a best-effort
// path — the legacy cipher is weak and only exists for migration compatibility.
func (s *SecretStore) decryptLegacy(value string) (string, error) {
	encoded := strings.TrimPrefix(value, "enc:")
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secret_store: invalid enc base64: %w", err)
	}

	// Legacy XOR cipher with a fixed key.
	legacyKey := []byte("mneme-legacy-xor-key-32byte!")
	decrypted := make([]byte, len(data))
	for i, b := range data {
		decrypted[i] = b ^ legacyKey[i%len(legacyKey)]
	}
	return string(decrypted), nil
}

// ── Key comparison ────────────────────────────────────────────────────

// MasterKeyEqual returns true if the two keys are identical (constant-time).
func MasterKeyEqual(a, b *[keyLen]byte) bool {
	if a == nil || b == nil {
		return false
	}
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
