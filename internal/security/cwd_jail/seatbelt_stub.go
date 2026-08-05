//go:build !darwin

package cwd_jail

// NewSeatbelt returns nil on non-macOS platforms.
func NewSeatbelt() Backend { return nil }
