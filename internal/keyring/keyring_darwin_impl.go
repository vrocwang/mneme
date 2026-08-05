//go:build darwin

package keyring

func platformKeyring(fallback *FileStore) Store {
	return newMacOSKeychain()
}
