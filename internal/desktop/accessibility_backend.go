package desktop

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// AccessibilityBackend provides platform-native UI introspection and
// manipulation via shell commands.
//
// Platform status:
//
//	macOS     — System Events osascript (full element tree). Requires
//	           "System Events" permission in System Preferences.
//	Windows   — UIAutomationClient via PowerShell (full element tree).
//	Linux     — xdotool window-name search only. Full AT-SPI2 tree
//	           traversal is not yet implemented; contributions welcome.
//
// The desktop-auto extension (extensions/desktop-auto/) provides an
// alternative backend with native FFI for applications that need
// higher performance or deeper platform integration.
type AccessibilityBackend interface {
	// GetFocus returns information about the currently focused UI element.
	GetFocus() (*FocusInfo, error)

	// InsertText types text at the current cursor position.
	InsertText(text string) error

	// PressKeys sends a key combination (e.g. "ctrl+c", "cmd+shift+t").
	PressKeys(keys string) error

	// Click clicks at the given screen coordinates.
	Click(x, y int) error

	// MoveMouse moves the cursor to the given screen coordinates.
	MoveMouse(x, y int) error

	// GetScreenSize returns the primary display dimensions.
	GetScreenSize() (width, height int, err error)

	// Platform returns the OS identifier ("darwin", "linux", "windows").
	Platform() string
}

// ShellAccessibility is the default AccessibilityBackend that delegates to
// platform-specific shell commands. Always available as fallback.
type ShellAccessibility struct {
	*Accessibility
}

// NewShellAccessibility creates a shell-based accessibility backend.
func NewShellAccessibility() *ShellAccessibility {
	return &ShellAccessibility{Accessibility: NewAccessibility()}
}

func (s *ShellAccessibility) PressKeys(keys string) error {
	return simulateKeyPress(keys)
}

func (s *ShellAccessibility) Click(x, y int) error {
	return simulateClick(x, y)
}

func (s *ShellAccessibility) MoveMouse(x, y int) error {
	return simulateMouseMove(x, y)
}

func (s *ShellAccessibility) GetScreenSize() (int, int, error) {
	return getScreenSize()
}

func (s *ShellAccessibility) Platform() string {
	if s.Accessibility == nil {
		return runtime.GOOS
	}
	return s.platform
}

// ── Platform-specific helpers ───────────────────────────────────────────

func simulateKeyPress(keys string) error {
	switch runtime.GOOS {
	case "darwin":
		escaped := strings.ReplaceAll(keys, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return exec.Command("osascript", "-e",
			fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)).Run()
	case "linux":
		return exec.Command("xdotool", "key", keys).Run()
	case "windows":
		escaped := strings.ReplaceAll(keys, `"`, `""`)
		script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait("%s")`, escaped)
		return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
	default:
		return fmt.Errorf("simulateKeyPress: unsupported platform %s", runtime.GOOS)
	}
}

func simulateClick(x, y int) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(
			`tell application "System Events" to click at {%d, %d}`, x, y)
		return exec.Command("osascript", "-e", script).Run()
	case "linux":
		return exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y),
			"click", "1").Run()
	case "windows":
		script := fmt.Sprintf(`
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Mouse {
[DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
[DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, int dwExtraInfo);
}
"@
[Mouse]::SetCursorPos(%d, %d)
[Mouse]::mouse_event(0x0002, 0, 0, 0, 0)
[Mouse]::mouse_event(0x0004, 0, 0, 0, 0)
`, x, y)
		return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
	default:
		return fmt.Errorf("simulateClick: unsupported platform %s", runtime.GOOS)
	}
}

func simulateMouseMove(x, y int) error {
	switch runtime.GOOS {
	case "darwin":
		// Move mouse cursor via CoreGraphics. cliclick is preferred when
		// available; fall back to a Python Quartz snippet (stock macOS).
		if _, err := exec.LookPath("cliclick"); err == nil {
			return exec.Command("cliclick", fmt.Sprintf("m:%d,%d", x, y)).Run()
		}
		script := fmt.Sprintf(
			`import Quartz; e=Quartz.CGEventCreateMouseEvent(None,Quartz.kCGEventMouseMoved,(%d,%d),0);Quartz.CGEventPost(Quartz.kCGHIDEventTap,e)`, x, y)
		return exec.Command("python3", "-c", script).Run()
	case "linux":
		return exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y)).Run()
	case "windows":
		script := fmt.Sprintf(`
Add-Type -Name Mouse -Namespace Win32 -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);'
[Win32.Mouse]::SetCursorPos(%d, %d)
`, x, y)
		return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
	default:
		return fmt.Errorf("simulateMouseMove: unsupported platform %s", runtime.GOOS)
	}
}

func getScreenSize() (int, int, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
		if err != nil {
			return 0, 0, err
		}
		return parseDisplayResolution(string(out))
	case "linux":
		out, err := exec.Command("xdotool", "getdisplaygeometry").Output()
		if err != nil {
			// Fallback to xrandr.
			out2, err2 := exec.Command("xrandr", "--current").Output()
			if err2 != nil {
				return 0, 0, fmt.Errorf("xdotool/xrandr: %w (xrandr: %v)", err, err2)
			}
			return parseXrandrResolution(string(out2))
		}
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) >= 2 {
			w, errW := strconv.Atoi(parts[0])
			h, errH := strconv.Atoi(parts[1])
			if errW == nil && errH == nil {
				return w, h, nil
			}
		}
		return 1920, 1080, fmt.Errorf("cannot parse xdotool geometry: %q", string(out))
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms
$screen = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
Write-Output "$($screen.Width) $($screen.Height)"`
		out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
		if err != nil {
			return 0, 0, err
		}
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) >= 2 {
			w, _ := strconv.Atoi(parts[0])
			h, _ := strconv.Atoi(parts[1])
			return w, h, nil
		}
		return 1920, 1080, nil
	default:
		return 1920, 1080, nil
	}
}

func parseDisplayResolution(output string) (int, int, error) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Resolution:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				res := strings.TrimSpace(parts[1])
				dims := strings.Split(res, " x ")
				if len(dims) == 2 {
					w, _ := strconv.Atoi(strings.TrimSpace(dims[0]))
					h, _ := strconv.Atoi(strings.TrimSpace(dims[1]))
					return w, h, nil
				}
			}
		}
	}
	return 1920, 1080, nil
}

func parseXrandrResolution(output string) (int, int, error) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// The connected display line looks like: "HDMI-0 connected 1920x1080+0+0"
		if strings.Contains(line, " connected ") {
			fields := strings.Fields(line)
			for _, f := range fields {
				if strings.Contains(f, "x") && strings.Contains(f, "+") {
					// Strip the offset: "1920x1080+0+0" → "1920x1080"
					res := f
					if idx := indexOfByte(f, '+'); idx >= 0 {
						res = f[:idx]
					}
					dims := strings.Split(res, "x")
					if len(dims) == 2 {
						w, _ := strconv.Atoi(dims[0])
						h, _ := strconv.Atoi(dims[1])
						return w, h, nil
					}
				}
			}
		}
	}
	return 1920, 1080, nil
}

func indexOfByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
