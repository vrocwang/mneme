// Package fs defines the filesystem seam: the minimal capability interface
// that file tools depend on. It is one of Mneme's cordis-style seams
// (Definition + Provider + Consumer); see docs/adr/0002-seam-specification.md.
//
// The Definition (FS) is this package's interface. OS is the in-process
// Provider backed by the host filesystem. Consumers (internal/tools file
// tools) depend only on the interface, never on OS; the provider is injected
// at the assembly points (internal/capability, internal/eino).
package fs

import (
	"io"
	"os"
)

// FS is the filesystem capability interface. Paths are absolute and already
// validated by the caller (security.ValidatePath); a provider performs no
// further confinement — that is the caller's responsibility.
type FS interface {
	// Open opens path for reading. The caller must close the returned handle.
	Open(path string) (io.ReadCloser, error)
	// ReadFile reads the whole file. Prefer Open for large or streamed reads.
	ReadFile(path string) ([]byte, error)
	// WriteFile writes data to path with the given permission, creating or
	// truncating the file.
	WriteFile(path string, data []byte, perm os.FileMode) error
	// MkdirAll creates path and any missing parents.
	MkdirAll(path string, perm os.FileMode) error
	// ReadDir lists the directory entries at path.
	ReadDir(path string) ([]os.DirEntry, error)
}

// OS is the in-process Provider backed by the host filesystem.
type OS struct{}

// Compile-time check that OS satisfies FS.
var _ FS = OS{}

func (OS) Open(path string) (io.ReadCloser, error) { return os.Open(path) }

func (OS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (OS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (OS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

func (OS) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }
