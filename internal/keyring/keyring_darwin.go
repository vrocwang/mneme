package keyring

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// MacOSKeychain implements Store backed by the macOS Keychain via the `security` CLI.
type MacOSKeychain struct {
	fileFallback *FileStore
}

func newMacOSKeychain() *MacOSKeychain {
	return &MacOSKeychain{
		fileFallback: NewFileStore(secretsDir()),
	}
}

func accountName(service, key string) string {
	return fmt.Sprintf("mneme-%s-%s", service, key)
}

// keychainOpTimeout bounds how long a `security` CLI invocation may block.
// The macOS keychain can hang indefinitely when locked or when access is
// denied in a headless/sandboxed environment; a bounded timeout turns that
// hang into a deterministic failure so callers fall back promptly.
const keychainOpTimeout = 5 * time.Second

// securityOutput runs `security` with a bounded timeout and returns stdout.
//
// The macOS `security` CLI can enter uninterruptible sleep when the keychain
// daemon is unreachable (locked keychain, headless/sandboxed runtime). In
// that state neither SIGKILL nor Cmd.WaitDelay can reap the process, so a
// plain Cmd.Output() would block forever in wait4. To stay responsive we run
// the command on a goroutine and abandon it (returning the context error)
// when the timeout elapses. The orphaned goroutine exits on its own once the
// OS eventually reaps the process; this only happens in already-broken
// keychain environments.
func securityOutput(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", args...)
	cmd.WaitDelay = keychainOpTimeout
	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := cmd.Output()
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, ctx.Err()
	}
}

// securityRun runs `security` with a bounded timeout (see securityOutput).
func securityRun(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", args...)
	cmd.WaitDelay = keychainOpTimeout
	ch := make(chan error, 1)
	go func() {
		ch <- cmd.Run()
	}()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return ctx.Err()
	}
}

// Probe reports whether the macOS Keychain is reachable. It performs a raw
// `security find-generic-password` for a sentinel item and returns true when
// the keychain responds - including the "item not found" exit code 44, which
// proves the keychain is functional. This deliberately bypasses Get's consent
// fallback to avoid the IsAvailable -> Get -> fallbackGet -> CheckSecretAccess
// -> IsAvailable recursion.
func (s *MacOSKeychain) Probe() bool {
	_, err := securityOutput("find-generic-password",
		"-s", "__mneme_probe__",
		"-a", accountName("__mneme_probe__", "__probe__"),
		"-w",
	)
	if err == nil {
		return true
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
		return true // item not found, but the keychain responded
	}
	return false
}

func (s *MacOSKeychain) Get(service, key string) (string, error) {
	out, err := securityOutput("find-generic-password",
		"-s", service,
		"-a", accountName(service, key),
		"-w",
	)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			// Item not found — fall back after consent check.
			return s.fallbackGet(service, key)
		}
		return "", ErrNotFound
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *MacOSKeychain) Set(service, key, value string) error {
	if err := securityRun("add-generic-password",
		"-s", service,
		"-a", accountName(service, key),
		"-w", value,
		"-U", // update if exists
	); err != nil {
		return s.fallbackSet(service, key, value)
	}
	return nil
}

func (s *MacOSKeychain) Delete(service, key string) error {
	if err := securityRun("delete-generic-password",
		"-s", service,
		"-a", accountName(service, key),
	); err != nil {
		return s.fallbackDelete(service, key)
	}
	return s.fallbackDelete(service, key)
}

// fallback* methods consult the consent gate before falling back to file storage.
func (s *MacOSKeychain) fallbackGet(service, key string) (string, error) {
	switch CheckSecretAccess() {
	case DecisionProceed:
		return s.fileFallback.Get(service, key)
	case DecisionDeclined:
		return "", fmt.Errorf("keyring: OS keyring unavailable, user declined local storage fallback")
	default:
		return "", fmt.Errorf("keyring: OS keyring unavailable, user consent required for local storage")
	}
}

func (s *MacOSKeychain) fallbackSet(service, key, value string) error {
	switch CheckSecretAccess() {
	case DecisionProceed:
		return s.fileFallback.Set(service, key, value)
	case DecisionDeclined:
		return fmt.Errorf("keyring: OS keyring unavailable, user declined local storage fallback")
	default:
		return fmt.Errorf("keyring: OS keyring unavailable, user consent required for local storage")
	}
}

func (s *MacOSKeychain) fallbackDelete(service, key string) error {
	switch CheckSecretAccess() {
	case DecisionProceed:
		return s.fileFallback.Delete(service, key)
	default:
		return nil
	}
}

func isNativeKeyringSupported() bool { return true }
