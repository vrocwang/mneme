// Package soul provides read/write access to the workspace identity files
// (SOUL.md and IDENTITY.md) for the frontend settings page.
package soul

import (
	"fmt"
	"os"
	"path/filepath"
)

// RPC exposes SOUL.md and IDENTITY.md editing to the Wails frontend.
type RPC struct {
	workspace string
}

// NewRPC creates a soul file RPC handler.
func NewRPC(workspace string) *RPC {
	return &RPC{workspace: workspace}
}

// GetSOUL returns the contents of SOUL.md.
func (r *RPC) GetSOUL() (string, error) {
	return r.readFile("SOUL.md")
}

// SetSOUL overwrites SOUL.md with the given content.
func (r *RPC) SetSOUL(content string) error {
	return r.writeFile("SOUL.md", content)
}

// GetIdentity returns the contents of IDENTITY.md.
func (r *RPC) GetIdentity() (string, error) {
	return r.readFile("IDENTITY.md")
}

// SetIdentity overwrites IDENTITY.md with the given content.
func (r *RPC) SetIdentity(content string) error {
	return r.writeFile("IDENTITY.md", content)
}

func (r *RPC) readFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.workspace, name))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	return string(data), nil
}

func (r *RPC) writeFile(name, content string) error {
	path := filepath.Join(r.workspace, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
