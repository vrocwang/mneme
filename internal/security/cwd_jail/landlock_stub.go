//go:build !linux

package cwd_jail

// NewLandlock returns nil on non-Linux platforms.
func NewLandlock() Backend { return nil }
