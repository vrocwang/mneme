//go:build linux
// +build linux

package overlay

import (
	"context"
	"fmt"
	"os/exec"
)

func showPlatform(ctx context.Context, win *Window) error {
	// On Linux, use zenity or yad for simple overlay notifications.
	// For full transparent overlay windows, GTK or Qt is needed.
	// This provides a basic notification-based fallback.
	text := win.Content.Text
	if text == "" {
		text = win.Content.HTML
	}
	if text == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "zenity",
		"--notification",
		"--text", text,
	)
	if err := cmd.Start(); err != nil {
		// Fallback to notify-send
		cmd2 := exec.CommandContext(ctx, "notify-send", "Mneme", text)
		if err2 := cmd2.Run(); err2 != nil {
			return fmt.Errorf("overlay: no display tool found (tried zenity, notify-send): %v", err2)
		}
	}
	return nil
}

func hidePlatform(id string) error {
	return nil // notification-based overlays auto-dismiss
}
