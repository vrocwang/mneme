// Package keyring provides ChaCha20-Poly1305 cryptographic helpers for
// the SecretStore (config field encryption) and EncryptedFileBackend
// (secrets file encryption).
package keyring

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	nonceLen = chacha20poly1305.NonceSizeX
	keyLen   = chacha20poly1305.KeySize
)

// chacha20Encrypt encrypts plaintext with ChaCha20-Poly1305 (XChaCha20 variant).
// Returns nonce || ciphertext || tag.
func chacha20Encrypt(key *[keyLen]byte, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305: %w", err)
	}

	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	blob := make([]byte, 0, nonceLen+len(ciphertext))
	blob = append(blob, nonce...)
	blob = append(blob, ciphertext...)
	return blob, nil
}

// chacha20Decrypt decrypts a nonce || ciphertext || tag blob produced by
// chacha20Encrypt. Returns an error on wrong key or tampered data.
func chacha20Decrypt(key *[keyLen]byte, blob []byte) ([]byte, error) {
	if len(blob) <= nonceLen {
		return nil, fmt.Errorf("encrypted blob too short (missing nonce)")
	}

	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305: %w", err)
	}

	nonce := blob[:nonceLen]
	ciphertext := blob[nonceLen:]

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed — wrong key or tampered data")
	}
	return plaintext, nil
}

// generateRandomKey generates a 32-byte cryptographically random key.
func generateRandomKey() (*[keyLen]byte, error) {
	var key [keyLen]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return &key, nil
}

// hexEncode encodes bytes as lowercase hex.
func hexEncode(data []byte) string {
	return hex.EncodeToString(data)
}

// hexDecode decodes a hex string into bytes.
func hexDecode(s string) ([]byte, error) {
	return hex.DecodeString(s)
}
