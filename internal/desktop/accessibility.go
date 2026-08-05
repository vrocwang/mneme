package desktop

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Accessibility provides platform-specific assistive capabilities:
// focus querying, text insertion, and window management.
type Accessibility struct {
	platform string
}

// NewAccessibility creates a platform-aware accessibility helper.
func NewAccessibility() *Accessibility {
	return &Accessibility{platform: runtime.GOOS}
}

// FocusInfo describes the currently focused UI element.
type FocusInfo struct {
	WindowTitle string `json:"window_title"`
	AppName     string `json:"app_name"`
	ProcessID   string `json:"process_id"`
	CursorText  string `json:"cursor_text,omitempty"`
}

// GetFocus returns information about the currently focused window/control.
// Uses OS-specific commands to query the accessibility API.
func (a *Accessibility) GetFocus() (*FocusInfo, error) {
	switch runtime.GOOS {
	case "darwin":
		return a.getFocusDarwin()
	case "linux":
		return a.getFocusLinux()
	case "windows":
		return a.getFocusWindows()
	default:
		return nil, fmt.Errorf("accessibility: unsupported platform %s", runtime.GOOS)
	}
}

// InsertText types text at the current cursor position via the accessibility API.
func (a *Accessibility) InsertText(text string) error {
	switch runtime.GOOS {
	case "darwin":
		return a.insertTextDarwin(text)
	case "linux":
		return a.insertTextLinux(text)
	case "windows":
		return a.insertTextWindows(text)
	default:
		return fmt.Errorf("accessibility: unsupported platform %s", runtime.GOOS)
	}
}

// ── Darwin (macOS) ──────────────────────────────────────────────

func (a *Accessibility) getFocusDarwin() (*FocusInfo, error) {
	// Use AppleScript to get the frontmost application info
	script := `tell application "System Events"
		set frontApp to first application process whose frontmost is true
		set appName to name of frontApp
		set windowTitle to title of front window of frontApp
		return appName & "|" & windowTitle
	end tell`

	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil, fmt.Errorf("osascript focus query: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	info := &FocusInfo{AppName: parts[0]}
	if len(parts) > 1 {
		info.WindowTitle = parts[1]
	}
	return info, nil
}

func (a *Accessibility) insertTextDarwin(text string) error {
	// Use osascript to send keystrokes via System Events
	escaped := escapeAppleScript(text)
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
	return exec.Command("osascript", "-e", script).Run()
}

// ── Linux ───────────────────────────────────────────────────────

func (a *Accessibility) getFocusLinux() (*FocusInfo, error) {
	// Use xdotool to get the active window
	out, err := exec.Command("xdotool", "getactivewindow", "getwindowname").Output()
	if err != nil {
		// Try wmctrl as fallback
		out2, err2 := exec.Command("wmctrl", "-a", ":ACTIVE:").Output()
		if err2 != nil {
			return nil, fmt.Errorf("xdotool/wmctrl focus query: %w (xdotool: %v)", err2, err)
		}
		return &FocusInfo{WindowTitle: strings.TrimSpace(string(out2))}, nil
	}
	return &FocusInfo{WindowTitle: strings.TrimSpace(string(out))}, nil
}

func (a *Accessibility) insertTextLinux(text string) error {
	// xdotool type
	return exec.Command("xdotool", "type", text).Run()
}

// ── Windows ─────────────────────────────────────────────────────

func (a *Accessibility) getFocusWindows() (*FocusInfo, error) {
	// PowerShell to get foreground window
	script := `Add-Type @"
using System;
using System.Runtime.InteropServices;
using System.Text;
public class Win32 {
	[DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
	[DllImport("user32.dll")] public static extern int GetWindowText(IntPtr hWnd, StringBuilder text, int count);
	[DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
}
"@
$hwnd = [Win32]::GetForegroundWindow()
$sb = New-Object System.Text.StringBuilder(256)
[Win32]::GetWindowText($hwnd, $sb, 256)
$pid = 0
[Win32]::GetWindowThreadProcessId($hwnd, [ref]$pid)
Write-Output "$($sb.ToString())|$pid"`

	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return nil, fmt.Errorf("powershell focus query: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	info := &FocusInfo{}
	if len(parts) > 0 {
		info.WindowTitle = parts[0]
	}
	if len(parts) > 1 {
		info.ProcessID = parts[1]
	}
	return info, nil
}

func (a *Accessibility) insertTextWindows(text string) error {
	// PowerShell SendKeys
	escaped := strings.ReplaceAll(text, `"`, `""`)
	script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait("%s")`, escaped)
	return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}

// ── Helpers ─────────────────────────────────────────────────────

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
