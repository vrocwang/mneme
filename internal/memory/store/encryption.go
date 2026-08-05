package store

import (
	"encoding/hex"
	"fmt"

	"github.com/simon/mneme/internal/security"
)

// MemoryEncryptor provides transparent encryption for memory content.
// When enabled, content is encrypted before storage and decrypted on retrieval.
type MemoryEncryptor struct {
	key []byte // 32-byte AES-256 key
}

// NewMemoryEncryptor creates an encryptor from a 32-byte key.
// Pass nil or empty to disable encryption (no-op mode).
func NewMemoryEncryptor(key []byte) *MemoryEncryptor {
	if len(key) != 32 {
		return &MemoryEncryptor{} // no-op mode
	}
	keyCopy := make([]byte, 32)
	copy(keyCopy, key)
	return &MemoryEncryptor{key: keyCopy}
}

// Enabled returns true if encryption is active.
func (e *MemoryEncryptor) Enabled() bool {
	return len(e.key) == 32
}

// EncryptContent encrypts content if encryption is enabled.
// Returns the encrypted content prefixed with "[ENC]" marker so we can
// detect encrypted data on read.
func (e *MemoryEncryptor) EncryptContent(plaintext string) (string, error) {
	if !e.Enabled() {
		return plaintext, nil
	}
	ciphertext, err := security.Encrypt(e.key, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("encrypt memory content: %w", err)
	}
	// Encode as hex for safe SQLite storage.
	return "[ENC]" + hex.EncodeToString(ciphertext), nil
}

// DecryptContent decrypts content if it was encrypted.
// Content without the "[ENC]" prefix is returned unchanged (backward compatible).
func (e *MemoryEncryptor) DecryptContent(stored string) (string, error) {
	if !e.Enabled() || len(stored) < 5 || stored[:5] != "[ENC]" {
		return stored, nil
	}
	hexData := stored[5:]
	ciphertext, err := hex.DecodeString(hexData)
	if err != nil {
		return "", fmt.Errorf("decode encrypted memory: %w", err)
	}
	plaintext, err := security.Decrypt(e.key, ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt memory content: %w", err)
	}
	return string(plaintext), nil
}
