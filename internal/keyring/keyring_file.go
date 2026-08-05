package keyring

import (
	"os"
	"path/filepath"

	"github.com/simon/mneme/internal/config"
)

type FileStore struct {
	dir string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) path(service, key string) string {
	return filepath.Join(s.dir, service+"_"+key+".enc")
}

func (s *FileStore) Get(service, key string) (string, error) {
	data, err := os.ReadFile(s.path(service, key))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	return string(data), nil
}

func (s *FileStore) Set(service, key, value string) error {
	return os.WriteFile(s.path(service, key), []byte(value), 0600)
}

func (s *FileStore) Delete(service, key string) error {
	err := os.Remove(s.path(service, key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// secretsDir returns the path to the secrets directory, derived from the workspace.
func secretsDir() string {
	workspace := config.WorkspaceDir()
	dir := filepath.Join(workspace, "secrets")
	os.MkdirAll(dir, 0700)
	return dir
}

// defaultStore returns the best available keyring for the current platform.
func defaultStore() Store {
	fileFallback := NewFileStore(secretsDir())
	return platformKeyring(fileFallback)
}
