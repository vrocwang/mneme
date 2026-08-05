package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

// ── Argon2id parameters ────────────────────────────────────────────
// Matches the Rust implementation: 64MB memory, 3 iterations, 1 parallelism.

const (
	argonMemory      = 64 * 1024 // 64 MB in KiB
	argonIterations  = 3
	argonParallelism = 1
	argonSaltLen     = 32
	argonKeyLen      = 32 // AES-256
)

// deriveKey derives a 32-byte AES-256 key from a password and salt using
// Argon2id. The parameters mirror the Rust encryption module.
func deriveKey(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)
}

// DeriveKey derives an encryption key from a seed string and workspace path.
// Used as the primary key derivation for the credential store.
// If seed is empty, the workspace path is used as the seed.
//
// A per-installation master salt is combined with the workspace path to produce
// a non-deterministic salt, preventing rainbow-table precomputation attacks
// against known workspace paths. The master salt is persisted to
// <workspace>/secrets/master_salt.bin and auto-generated on first use.
func DeriveKey(seed, workspace string) []byte {
	if seed == "" {
		seed = workspace
	}
	masterSalt := getOrCreateMasterSalt(workspace)
	// Combine master salt (random per-install) with workspace (deterministic)
	// so the salt is both unique and recoverable across restarts.
	material := append(masterSalt, []byte(":"+workspace)...)
	salt := sha256.Sum256(material)
	return deriveKey([]byte(seed), salt[:])
}

// getOrCreateMasterSalt returns a per-installation random salt, generating and
// persisting one on first use.
func getOrCreateMasterSalt(workspace string) []byte {
	saltPath := filepath.Join(workspace, "secrets", "master_salt.bin")
	data, err := os.ReadFile(saltPath)
	if err == nil {
		if len(data) == 32 {
			return data
		}
		// Corrupted salt file — DO NOT silently regenerate. A new salt would
		// make all previously encrypted data permanently undecryptable.
		// Log aggressively and return the corrupted data so the caller gets
		// a consistently-wrong key rather than a silently-different one.
		fmt.Fprintf(os.Stderr, "CRITICAL: master salt file %s has wrong length (%d bytes, expected 32). "+
			"All previously encrypted data may be unrecoverable. "+
			"Restore this file from backup or re-encrypt all data after fixing.\n",
			saltPath, len(data))
		// Return a hash of the corrupted data so the key is at least
		// deterministic (same wrong salt → same wrong key every time),
		// giving the user a chance to restore the correct salt file.
		h := sha256.Sum256(append([]byte("mneme-corrupted-salt:"), data...))
		return h[:]
	}
	if !os.IsNotExist(err) {
		// Read error (permissions, etc.) — fall back to a deterministic
		// workspace-bound salt for this session only.
		fmt.Fprintf(os.Stderr, "WARNING: cannot read master salt file %s: %v — using session-only fallback\n", saltPath, err)
		h := sha256.Sum256([]byte("mneme-salt-read-error:" + workspace))
		return h[:]
	}
	// File does not exist — generate a fresh 32-byte random salt.
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		// Fallback: if crypto/rand fails, use a deterministic fallback
		// that at least binds to this specific workspace.
		h := sha256.Sum256([]byte("mneme-master-salt-fallback:" + workspace))
		return h[:]
	}
	// Best-effort write — if it fails, we still have the in-memory salt
	// for this session.
	os.MkdirAll(filepath.Dir(saltPath), 0700)
	os.WriteFile(saltPath, salt, 0600)
	return salt
}

// ── Encrypted payload ──────────────────────────────────────────────

// EncryptedPayload holds ciphertext with metadata needed for decryption.
// Serialised to JSON for storage. Mirrors the Rust EncryptedPayload struct.
type EncryptedPayload struct {
	Ciphertext string `json:"ciphertext"` // base64-encoded nonce + ciphertext
	Nonce      string `json:"nonce"`      // base64-encoded nonce
	Salt       string `json:"salt"`       // base64-encoded salt
}

// EncryptString encrypts plaintext with a password, returning a serialisable
// payload. A random 32-byte salt is generated for each encryption.
func EncryptString(password string, plaintext string) (*EncryptedPayload, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key := deriveKey([]byte(password), salt)
	nonce, ciphertext, err := encrypt(key, []byte(plaintext))
	if err != nil {
		return nil, err
	}

	return &EncryptedPayload{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Salt:       base64.StdEncoding.EncodeToString(salt),
	}, nil
}

// DecryptString decrypts a payload produced by EncryptString.
func DecryptString(password string, payload *EncryptedPayload) (string, error) {
	salt, err := base64.StdEncoding.DecodeString(payload.Salt)
	if err != nil {
		return "", fmt.Errorf("decode salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(payload.Nonce)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	key := deriveKey([]byte(password), salt)
	plaintext, err := decrypt(key, nonce, ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// MarshalPayload serialises an EncryptedPayload to JSON.
func MarshalPayload(p *EncryptedPayload) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalPayload deserialises an EncryptedPayload from JSON.
func UnmarshalPayload(data []byte) (*EncryptedPayload, error) {
	var p EncryptedPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ── Key file management ────────────────────────────────────────────

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

// ── Core encrypt/decrypt ───────────────────────────────────────────

func encrypt(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("aes: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("gcm: %w", err)
	}
	nonce = make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = aesGCM.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

func decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return aesGCM.Open(nil, nonce, ciphertext, nil)
}
