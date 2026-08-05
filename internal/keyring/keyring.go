package keyring

import "errors"

var ErrNotFound = errors.New("keyring: key not found")

type Store interface {
	Get(service, key string) (string, error)
	Set(service, key, value string) error
	Delete(service, key string) error
}

// Prober is an optional capability implemented by OS-keyring backends.
// Probe reports whether the native keyring is reachable WITHOUT invoking
// the Get/Set fallback path (which consults the consent gate and would
// otherwise recurse back into IsAvailable). Backends that lack a native
// keyring (FileStore, EncryptedFileBackend) do not implement Prober and
// fall back to a Get-based probe, which is safe for them because their
// Get never calls back into the consent gate.
type Prober interface {
	Probe() bool
}

func Default() Store {
	return defaultStore()
}
