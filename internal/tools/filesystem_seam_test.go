package tools

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	mnfs "github.com/simon/mneme/internal/fs"
)

// memFS is an in-memory filesystem provider used to prove the file tools are
// seam-agnostic: the same Consumer runs against either provider.
type memFS struct {
	files map[string][]byte
}

func newMemFS() *memFS { return &memFS{files: map[string][]byte{}} }

func (m *memFS) Open(path string) (io.ReadCloser, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &memFile{data: data}, nil
}

func (m *memFS) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (m *memFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	m.files[path] = append([]byte(nil), data...)
	return nil
}

func (m *memFS) MkdirAll(string, os.FileMode) error { return nil }

func (m *memFS) ReadDir(path string) ([]os.DirEntry, error) {
	var out []os.DirEntry
	for p := range m.files {
		if filepath.Dir(p) == path {
			out = append(out, memDirEntry{name: filepath.Base(p)})
		}
	}
	return out, nil
}

type memFile struct{ data []byte; pos int }

func (f *memFile) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *memFile) Close() error { return nil }

type memDirEntry struct{ name string }

func (e memDirEntry) Name() string               { return e.name }
func (e memDirEntry) IsDir() bool                { return false }
func (e memDirEntry) Type() os.FileMode          { return 0 }
func (e memDirEntry) Info() (os.FileInfo, error) { return nil, nil }

// TestFileTools_SeamAgnostic runs the same Consumers (write_file/read_file/
// list_dir) against both the in-process OS provider and an in-memory provider,
// proving the tools depend only on the fs.FS interface (seam Definition).
func TestFileTools_SeamAgnostic(t *testing.T) {
	tmp := t.TempDir()
	providers := map[string]mnfs.FS{
		"os": mnfs.OS{},
		"memory": func() mnfs.FS {
			m := newMemFS()
			m.files[filepath.Join(tmp, "seed.txt")] = []byte("seed")
			return m
		}(),
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			w := NewWriteFileWithFS(tmp, provider)
			r := NewReadFileWithFS(tmp, provider)
			l := NewListDirWithFS(tmp, provider)

			ctx := context.Background()

			if res := w.Execute(ctx, map[string]interface{}{"path": "a.txt", "content": "hello"}); !res.Success {
				t.Fatalf("write_file: %s", res.Error)
			}
			if res := r.Execute(ctx, map[string]interface{}{"path": "a.txt"}); !res.Success || res.Output != "hello" {
				t.Fatalf("read_file: success=%v output=%q error=%s", res.Success, res.Output, res.Error)
			}
			if res := l.Execute(ctx, map[string]interface{}{"path": "."}); !res.Success {
				t.Fatalf("list_dir: %s", res.Error)
			}
		})
	}
}
