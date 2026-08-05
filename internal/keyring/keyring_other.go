//go:build !darwin && !linux

package keyring

// platformKeyring returns the file-based store on unsupported platforms (e.g. Windows).
func platformKeyring(fallback *FileStore) Store {
	return fallback
}

// isNativeKeyringSupported returns false on platforms without an OS keyring.
// When there's no native keyring, consent is auto-granted for file storage.
func isNativeKeyringSupported() bool {
	return false
}
