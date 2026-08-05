//go:build !linux
// +build !linux

package overlay

import "context"

func showPlatform(ctx context.Context, win *Window) error {
	return nil // no-op on unsupported platforms
}

func hidePlatform(id string) error {
	return nil // no-op on unsupported platforms
}
