package desktop

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// sensitiveAppDenylist mirrors the Rust accessibility denylist. Tools refuse
// to interact with these applications regardless of approval tier.
var sensitiveAppDenylist = []string{
	// Password managers
	"1password", "bitwarden", "lastpass", "dashlane", "keychain",
	// System settings
	"system preferences", "system settings", "settings",
	// Terminal emulators (arbitrary command injection risk)
	"terminal", "iterm", "kitty", "alacritty", "warp", "wezterm",
	"hyper", "windows terminal", "cmd.exe", "powershell",
}

// IsSensitiveApp returns true if the app name matches the denylist.
// Uses case-insensitive substring matching. An empty app name is never
// sensitive (it would otherwise match the first denylist entry via the
// empty-string substring).
func IsSensitiveApp(appName string) (bool, string) {
	lower := strings.ToLower(strings.TrimSpace(appName))
	if lower == "" {
		return false, ""
	}
	for _, denied := range sensitiveAppDenylist {
		if strings.Contains(lower, denied) || strings.Contains(denied, lower) {
			return true, denied
		}
	}
	return false, ""
}

// clampCoord ensures x,y are within the valid screen coordinate range.
// Rust enigo uses 0..32768; we use a generous 0..16384 for safety.
func clampCoord(x, y int) (int, int) {
	const maxCoord = 16384
	if x < 0 {
		x = 0
	}
	if x > maxCoord {
		x = maxCoord
	}
	if y < 0 {
		y = 0
	}
	if y > maxCoord {
		y = maxCoord
	}
	return x, y
}

// ValidateClickCoords returns an error if coordinates are out of bounds.
func ValidateClickCoords(x, y int) error {
	if x < 0 || x > 16384 || y < 0 || y > 16384 {
		return fmt.Errorf("coordinates (%d,%d) out of valid range (0..16384)", x, y)
	}
	return nil
}

// SettleWait polls the accessibility element count until it stabilises,
// matching the Rust ax_wait_settled behaviour. Polls at 300ms intervals,
// declares settled after 2 consecutive identical counts. Falls back to
// the given duration if AX is unavailable. 2-second deadline.
func SettleWait(ctx context.Context, ax *AXInteract, fallback time.Duration) {
	if ax == nil {
		time.Sleep(fallback)
		return
	}

	deadline := time.Now().Add(2 * time.Second)
	var lastCount int = -1
	stableFor := 0
	const minStableTicks = 2

	for time.Now().Before(deadline) {
		currentCount := ax.CountElements()
		if currentCount < 0 {
			time.Sleep(fallback) // AX unavailable, use fallback
			return
		}

		if currentCount == lastCount && lastCount > 0 {
			stableFor++
			if stableFor >= minStableTicks {
				time.Sleep(200 * time.Millisecond) // grace period after settle
				return
			}
		} else if currentCount != lastCount {
			stableFor = 0
			lastCount = currentCount
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
	// Deadline reached without settling; proceed anyway.
}
