package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// MaxFailedAttempts is the number of failed auth attempts before lockout.
	MaxFailedAttempts = 5
	// LockoutDuration is how long the guard refuses all attempts after rate limit is hit.
	LockoutDuration = 5 * time.Minute
	// TokenPrefix is prepended to bearer tokens for identification.
	TokenPrefix = "zc_"
)

// PairingGuard manages secure token-based authentication for RPC access.
// It generates a per-launch bearer token, persists its SHA-256 hash to disk
// for validation, and authenticates inbound requests against known tokens.
type PairingGuard struct {
	mu             sync.RWMutex
	hashedTokens   map[string]bool // SHA-256 hashed tokens
	tokenPath      string
	createdAt      time.Time
	failedAttempts int
	lockedUntil    time.Time
	log            *slog.Logger
}

// NewPairingGuard generates a fresh bearer token and persists its hash.
func NewPairingGuard(workspace string, log *slog.Logger) (*PairingGuard, error) {
	tokenPath := filepath.Join(workspace, "core.token")
	pg := &PairingGuard{
		hashedTokens: make(map[string]bool),
		tokenPath:    tokenPath,
		createdAt:    time.Now().UTC(),
		log:          log,
	}

	// Generate token with prefix for identification.
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	pg.hashedTokens[hashToken(token)] = true

	// Write the token to disk for frontend discovery (plaintext needed for initial auth).
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		return nil, fmt.Errorf("create token dir: %w", err)
	}
	if err := os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		return nil, fmt.Errorf("write token file: %w", err)
	}

	// Load any existing token hashes from a persistent file for cross-restart auth.
	pg.loadPersistedHashes(workspace)

	log.Info("pairing token generated", "path", tokenPath)
	return pg, nil
}

// generateToken creates a random 64-char hex bearer token with identification prefix.
func generateToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("crypto/rand.Read failed: %w", err)
	}
	return TokenPrefix + hex.EncodeToString(tokenBytes), nil
}

// hashToken returns the SHA-256 hex digest of a token.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// loadPersistedHashes loads previously authorized token hashes from disk.
func (pg *PairingGuard) loadPersistedHashes(workspace string) {
	hashPath := filepath.Join(workspace, "secrets", "token_hashes")
	data, err := os.ReadFile(hashPath)
	if err != nil {
		return // no persisted hashes
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			pg.hashedTokens[line] = true
		}
	}
}

// persistHashes writes the current token hash set to disk for cross-restart auth.
// Errors are logged but not returned — persistence is best-effort for token recovery.
func (pg *PairingGuard) persistHashes() {
	hashPath := filepath.Join(filepath.Dir(pg.tokenPath), "secrets", "token_hashes")
	var lines []string
	for h := range pg.hashedTokens {
		lines = append(lines, h)
	}
	data := strings.Join(lines, "\n")
	if err := os.MkdirAll(filepath.Dir(hashPath), 0700); err != nil {
		pg.log.Warn("failed to create token hashes dir", "path", filepath.Dir(hashPath), "error", err)
		return
	}
	if err := os.WriteFile(hashPath, []byte(data), 0600); err != nil {
		pg.log.Warn("failed to persist token hashes", "path", hashPath, "error", err)
	}
}

// Validate checks whether the provided token matches any known bearer.
// Uses constant-time comparison and enforces brute-force lockout.
// Uses an exclusive lock because the failure path mutates failedAttempts and
// lockedUntil; the common success path is read-only but Go's RWMutex does not
// support lock upgrading.
func (pg *PairingGuard) Validate(provided string) bool {
	if pg == nil || provided == "" {
		return false
	}

	pg.mu.Lock()
	defer pg.mu.Unlock()

	// Check lockout.
	if time.Now().Before(pg.lockedUntil) {
		pg.log.Warn("pairing validation blocked by lockout", "until", pg.lockedUntil)
		return false
	}

	// Lockout period has expired — reset failed attempts so a single
	// subsequent failure doesn't immediately re-trigger another lockout.
	if pg.failedAttempts >= MaxFailedAttempts {
		pg.failedAttempts = 0
	}

	// Use constant-time comparison to avoid timing side-channel.
	// Collect stored hashes into a slice so iteration order does not
	// leak via map-key hashing.
	providedHash := hashToken(provided)
	var found bool
	for _, h := range pg.hashedSlice() {
		if subtle.ConstantTimeCompare([]byte(h), []byte(providedHash)) == 1 {
			found = true
			break
		}
	}
	if found {
		pg.failedAttempts = 0
		return true
	}

	pg.failedAttempts++
	if pg.failedAttempts >= MaxFailedAttempts {
		pg.lockedUntil = time.Now().Add(LockoutDuration)
		pg.log.Warn("pairing brute-force lockout triggered",
			"attempts", pg.failedAttempts,
			"locked_until", pg.lockedUntil)
	}
	return false
}

// TokenPath returns the path where the bearer token is persisted.
func (pg *PairingGuard) TokenPath() string {
	if pg == nil {
		return ""
	}
	return pg.tokenPath
}

// AddToken authorizes an additional bearer token (e.g., from pairing flow).
// The token hash is persisted to survive restarts.
func (pg *PairingGuard) AddToken(token string) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	pg.hashedTokens[hashToken(token)] = true
	pg.persistHashes()
}

// Revoke invalidates all tokens and removes the token file.
func (pg *PairingGuard) Revoke() error {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	pg.hashedTokens = make(map[string]bool)
	if err := os.Remove(pg.tokenPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove token file: %w", err)
	}
	pg.log.Info("pairing tokens revoked")
	return nil
}

// Rotate generates a new token, revokes all old tokens, and writes the new one.
func (pg *PairingGuard) Rotate() error {
	newToken, err := generateToken()
	if err != nil {
		return err
	}

	pg.mu.Lock()
	pg.hashedTokens = make(map[string]bool)
	pg.hashedTokens[hashToken(newToken)] = true
	pg.createdAt = time.Now().UTC()
	pg.mu.Unlock()

	if err := os.WriteFile(pg.tokenPath, []byte(newToken), 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	pg.persistHashes()
	pg.log.Info("pairing token rotated (old tokens revoked)")
	return nil
}

// LoadTokenFromEnv reads an operator-supplied bearer from MNEME_CORE_TOKEN.
func LoadTokenFromEnv() (string, bool) {
	if t := os.Getenv("MNEME_CORE_TOKEN"); t != "" {
		return t, true
	}
	return "", false
}

// Cleanup removes the token file. Called on graceful shutdown.
func (pg *PairingGuard) Cleanup() {
	if pg == nil {
		return
	}
	if err := os.Remove(pg.tokenPath); err != nil && !os.IsNotExist(err) {
		pg.log.Warn("failed to remove token file", "path", pg.tokenPath, "error", err)
	}
}

// ValidateConstantTime performs constant-time token comparison without lockout semantics.
// Used for hot-path RPC checks where lockout is not desired.
func (pg *PairingGuard) ValidateConstantTime(provided string) bool {
	if pg == nil {
		return false
	}
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	providedHash := hashToken(provided)

	// Collect stored hashes into a slice so iteration order does not
	// leak via map-key hashing. Compare against every token without
	// early-return to avoid timing side-channels.
	var match int32
	for _, h := range pg.hashedSlice() {
		if subtle.ConstantTimeCompare([]byte(h), []byte(providedHash)) == 1 {
			match = 1
		}
	}
	return match == 1
}

// hashedSlice returns stored token hashes as a slice.
func (pg *PairingGuard) hashedSlice() []string {
	out := make([]string, 0, len(pg.hashedTokens))
	for h := range pg.hashedTokens {
		out = append(out, h)
	}
	return out
}
