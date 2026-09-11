package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// This file carries the desktop safety controls that used to live in the core
// internal/desktop package. They moved here with the automation primitives so
// the guard travels with the code it protects: the core no longer injects
// input, so it can no longer enforce a denylist.

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
//
// Matching is case-insensitive and DIRECTIONAL: the probed app name must
// contain a denylist entry. The reverse containment test is deliberately not
// used — it made short app names match by being a substring of a denylist
// entry (an app named "Pass" or "Key" matched "1password"/"keychain").
// Entries ending in ".exe" also match the bare process name, because Windows
// process names ("cmd", "powershell") omit the extension.
func IsSensitiveApp(appName string) (bool, string) {
	lower := strings.ToLower(strings.TrimSpace(appName))
	if lower == "" {
		return false, ""
	}
	for _, denied := range sensitiveAppDenylist {
		if strings.Contains(lower, denied) {
			return true, denied
		}
		if bare := strings.TrimSuffix(denied, ".exe"); bare != denied && strings.Contains(lower, bare) {
			return true, denied
		}
	}
	return false, ""
}

// clampCoord bounds coordinates to a generous SYMMETRIC range. Negative values
// are legitimate: on a multi-monitor setup a display placed left of or above
// the primary has negative virtual-screen coordinates, so clamping to >= 0
// would silently redirect those clicks to the primary display's origin.
func clampCoord(x, y int) (int, int) {
	const maxCoord = 16384
	if x < -maxCoord {
		x = -maxCoord
	} else if x > maxCoord {
		x = maxCoord
	}
	if y < -maxCoord {
		y = -maxCoord
	} else if y > maxCoord {
		y = maxCoord
	}
	return x, y
}

// guardFrontmostApp refuses to inject input when the actual frontmost
// application is in the sensitive denylist. It checks the real frontmost app,
// not an LLM-supplied description, which the model controls and could use to
// mask a sensitive target. Fails open when the frontmost app cannot be
// determined (no accessibility support, headless, unsupported platform).
func guardFrontmostApp() error {
	appName := frontmostAppName()
	if appName == "" {
		return nil
	}
	if denied, match := IsSensitiveApp(appName); denied {
		return fmt.Errorf("refusing to inject input: frontmost app %q matches denylist %q", appName, match)
	}
	return nil
}

// frontmostCacheTTL bounds how often the frontmost application is probed. The
// probe spawns a platform helper (osascript/powershell), and the guard runs on
// every mouse/keyboard action — including each step of an automate plan — so an
// uncached probe would add a subprocess spawn per action.
const frontmostCacheTTL = 1 * time.Second

var (
	frontmostMu   sync.Mutex
	frontmostName string
	frontmostAt   time.Time
)

// frontmostAppName returns the name of the frontmost application, or "" when it
// cannot be determined on this platform. The result is cached briefly so a
// rapid sequence of input actions does not spawn a probe per action.
//
// Cache caveat: within the TTL the guard can be bypassed by focusing a
// denylisted application and injecting input immediately. The TTL is a
// deliberate trade-off — an uncached probe spawns a helper process per mouse
// move — and the guard is a defence-in-depth control, not the only one.
func frontmostAppName() string {
	frontmostMu.Lock()
	defer frontmostMu.Unlock()
	if time.Since(frontmostAt) < frontmostCacheTTL {
		return frontmostName
	}
	frontmostName = probeFrontmostApp()
	frontmostAt = time.Now()
	return frontmostName
}

// probeFrontmostApp performs the platform-specific frontmost-app lookup.
func probeFrontmostApp() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("osascript", "-e",
			`tell application "System Events" to return name of first application process whose frontmost is true`,
		).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	case "windows":
		// Resolve the foreground PROCESS name, not the window title: a title is
		// arbitrary user/browser content, so a tab titled "Settings" would
		// otherwise block all input injection.
		out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			`Add-Type -Name W -Namespace T -MemberDefinition '[DllImport("user32.dll")]public static extern IntPtr GetForegroundWindow();[DllImport("user32.dll")]public static extern uint GetWindowThreadProcessId(IntPtr h, out uint p);';$h=[T.W]::GetForegroundWindow();$p=0;[void][T.W]::GetWindowThreadProcessId($h,[ref]$p);(Get-Process -Id $p).ProcessName`,
		).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}
