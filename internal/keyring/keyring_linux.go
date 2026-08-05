package keyring

import (
	"fmt"
	"os/exec"
	"strings"
)

// SecretServiceStore implements Store backed by the freedesktop.org Secret Service
// via the `secret-tool` CLI (part of libsecret-tools).
type SecretServiceStore struct {
	fileFallback *FileStore
}

func newSecretServiceStore() *SecretServiceStore {
	return &SecretServiceStore{
		fileFallback: NewFileStore(secretsDir()),
	}
}

// isSecretToolAvailable checks if secret-tool is on PATH.
func isSecretToolAvailable() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

// Probe reports whether the OS keyring backend is available. It checks for
// the secret-tool binary on PATH without invoking the Get/Set fallback path
// (which would recurse via CheckSecretAccess -> IsAvailable). Real Get/Set
// operations verify the daemon is reachable at use time and fall back if it
// is not.
func (s *SecretServiceStore) Probe() bool {
	return isSecretToolAvailable()
}

func (s *SecretServiceStore) Get(service, key string) (string, error) {
	if !isSecretToolAvailable() {
		return s.fallbackGet(service, key)
	}

	cmd := exec.Command("secret-tool", "lookup",
		"service", service,
		"key", key,
		"application", "mneme-go",
	)
	out, err := cmd.Output()
	if err != nil {
		return s.fallbackGet(service, key)
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *SecretServiceStore) Set(service, key, value string) error {
	if !isSecretToolAvailable() {
		return s.fallbackSet(service, key, value)
	}

	label := fmt.Sprintf("Mneme: %s/%s", service, key)
	cmd := exec.Command("secret-tool", "store",
		"--label", label,
		"service", service,
		"key", key,
		"application", "mneme-go",
	)
	cmd.Stdin = strings.NewReader(value)
	if err := cmd.Run(); err != nil {
		return s.fallbackSet(service, key, value)
	}
	return nil
}

func (s *SecretServiceStore) Delete(service, key string) error {
	if !isSecretToolAvailable() {
		return s.fallbackDelete(service, key)
	}

	cmd := exec.Command("secret-tool", "clear",
		"service", service,
		"key", key,
		"application", "mneme-go",
	)
	cmd.Run() // best-effort
	return s.fallbackDelete(service, key)
}

// fallback* methods consult the consent gate before falling back to file storage.
func (s *SecretServiceStore) fallbackGet(service, key string) (string, error) {
	switch CheckSecretAccess() {
	case DecisionProceed:
		return s.fileFallback.Get(service, key)
	case DecisionDeclined:
		return "", fmt.Errorf("keyring: OS keyring unavailable, user declined local storage fallback")
	default:
		return "", fmt.Errorf("keyring: OS keyring unavailable, user consent required for local storage")
	}
}

func (s *SecretServiceStore) fallbackSet(service, key, value string) error {
	switch CheckSecretAccess() {
	case DecisionProceed:
		return s.fileFallback.Set(service, key, value)
	case DecisionDeclined:
		return fmt.Errorf("keyring: OS keyring unavailable, user declined local storage fallback")
	default:
		return fmt.Errorf("keyring: OS keyring unavailable, user consent required for local storage")
	}
}

func (s *SecretServiceStore) fallbackDelete(service, key string) error {
	switch CheckSecretAccess() {
	case DecisionProceed:
		return s.fileFallback.Delete(service, key)
	default:
		return nil // deletion is non-critical; don't block on consent
	}
}

func isNativeKeyringSupported() bool { return true }
