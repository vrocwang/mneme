//go:build linux

package keyring

func platformKeyring(fallback *FileStore) Store {
	if isSecretToolAvailable() {
		return newSecretServiceStore()
	}
	return fallback
}
