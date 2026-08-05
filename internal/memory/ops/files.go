// Package ops provides memory file operations matching Rust memory/ops/files.rs.
// Implements sandboxed read/write/list within the workspace memory directory.
package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MemoryDir returns the memory subdirectory within the workspace.
func MemoryDir(workspaceDir string) string {
	return filepath.Join(workspaceDir, "memory")
}

// ValidateMemoryRelativePath rejects absolute paths and path traversal.
func ValidateMemoryRelativePath(path string) error {
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths are not allowed")
	}
	cleaned := filepath.Clean(path)
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("path traversal is not allowed")
	}
	return nil
}

// ResolveExistingMemoryPath validates and resolves a relative path within the memory directory.
func ResolveExistingMemoryPath(workspaceDir, relativePath string) (string, error) {
	if err := ValidateMemoryRelativePath(relativePath); err != nil {
		return "", err
	}
	full := filepath.Join(MemoryDir(workspaceDir), relativePath)
	// Ensure resolved path stays within memory root.
	memRoot := MemoryDir(workspaceDir)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, memRoot) {
		return "", fmt.Errorf("path escapes memory directory")
	}
	// Reject symlinks.
	if fi, err := os.Lstat(abs); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlinks are not allowed")
	}
	return abs, nil
}

// ResolveWritableMemoryPath validates and creates parent directories for a writable path.
func ResolveWritableMemoryPath(workspaceDir, relativePath string) (string, error) {
	if err := ValidateMemoryRelativePath(relativePath); err != nil {
		return "", err
	}
	full := filepath.Join(MemoryDir(workspaceDir), relativePath)
	memRoot := MemoryDir(workspaceDir)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, memRoot) {
		return "", fmt.Errorf("path escapes memory directory")
	}
	// Create parent directories.
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", err
	}
	// Reject symlinks.
	if fi, err := os.Lstat(abs); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinks are not allowed")
		}
	}
	return abs, nil
}

// ListMemoryFiles returns regular files in a memory subdirectory, skipping
// SQLite artifacts, symlinks, and subdirectories.
func ListMemoryFiles(workspaceDir, relativeDir string) ([]string, error) {
	if err := ValidateMemoryRelativePath(relativeDir); err != nil {
		return nil, err
	}
	dir := filepath.Join(MemoryDir(workspaceDir), relativeDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	skipFiles := map[string]bool{
		"memory.db":     true,
		"memory.db-shm": true,
		"memory.db-wal": true,
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if skipFiles[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

// ReadMemoryFile reads the content of a file within the memory directory.
func ReadMemoryFile(workspaceDir, relativePath string) (string, error) {
	abs, err := ResolveExistingMemoryPath(workspaceDir, relativePath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteMemoryFile writes content to a file within the memory directory.
// Returns the number of bytes written.
func WriteMemoryFile(workspaceDir, relativePath, content string) (int, error) {
	abs, err := ResolveWritableMemoryPath(workspaceDir, relativePath)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return 0, err
	}
	return len(content), nil
}
