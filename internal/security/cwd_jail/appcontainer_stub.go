//go:build !windows

package cwd_jail

// NewAppContainer returns nil on non-Windows platforms.
func NewAppContainer() Backend { return nil }
