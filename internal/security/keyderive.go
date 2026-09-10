package security

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// argonKeyLen is the AES-256 key length used for workspace key files.
const argonKeyLen = 32

// KeyFilePath returns the path to the encryption key file for a workspace.
func KeyFilePath(workspace string) string {
	return filepath.Join(workspace, "secrets", "encryption.key")
}

// LoadOrCreateKey loads an existing encryption key file or creates a new one.
// Returns the raw 32-byte key. If the key file exists but is corrupted (wrong
// length), it returns an error to prevent silent data loss.
func LoadOrCreateKey(workspace string) ([]byte, error) {
	keyPath := KeyFilePath(workspace)
	data, err := os.ReadFile(keyPath)
	if err == nil {
		if len(data) == argonKeyLen {
			return data, nil
		}
		return nil, fmt.Errorf("encryption key file %s has wrong length (%d bytes, expected %d) — file may be corrupted; refusing to overwrite to prevent data loss", keyPath, len(data), argonKeyLen)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	// Generate a new random key (only when file does not exist).
	key := make([]byte, argonKeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("write key file: %w", err)
	}

	return key, nil
}
