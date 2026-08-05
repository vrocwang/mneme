package desktop

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// TextInput provides OS-level text field read/write capabilities.
type TextInput struct{}

// NewTextInput creates a platform-aware text input helper.
func NewTextInput() *TextInput {
	return &TextInput{}
}

// ReadCursorText reads the text content at the current cursor position
// using the platform's accessibility API.
func (t *TextInput) ReadCursorText() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return t.readCursorDarwin()
	case "linux":
		return t.readCursorLinux()
	case "windows":
		return t.readCursorWindows()
	default:
		return "", fmt.Errorf("text_input: unsupported platform %s", runtime.GOOS)
	}
}

// InsertTextAtCursor types text at the current cursor position.
func (t *TextInput) InsertTextAtCursor(text string) error {
	a := NewAccessibility()
	return a.InsertText(text)
}

// ReplaceSelection replaces the currently selected text.
func (t *TextInput) ReplaceSelection(text string) error {
	// On most platforms, inserting text replaces the current selection
	return t.InsertTextAtCursor(text)
}

// ── Darwin ──────────────────────────────────────────────────────

func (t *TextInput) readCursorDarwin() (string, error) {
	// Use osascript to copy selected text, or get field value
	// This is a best-effort approach via clipboard
	script := `tell application "System Events"
		try
			set frontApp to first application process whose frontmost is true
			set focusedElement to focused UI element of frontApp
			return value of focusedElement
		on error
			return ""
		end try
	end tell`

	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", fmt.Errorf("osascript cursor text read: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ── Linux ───────────────────────────────────────────────────────

func (t *TextInput) readCursorLinux() (string, error) {
	// xdotool doesn't support reading text directly.
	// Use clipboard as proxy: copy selection, read clipboard.
	// First, simulate Ctrl+C to copy
	if err := exec.Command("xdotool", "key", "ctrl+c").Run(); err != nil {
		return "", err
	}
	// Wait briefly for clipboard to update
	// Read from xclip
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		// Try xsel as alternative
		out, err = exec.Command("xsel", "--clipboard", "--output").Output()
		if err != nil {
			return "", fmt.Errorf("xclip/xsel cursor read: %w", err)
		}
	}
	return strings.TrimSpace(string(out)), nil
}

// ── Windows ─────────────────────────────────────────────────────

func (t *TextInput) readCursorWindows() (string, error) {
	// PowerShell: get selected text from focused control, or clipboard copy
	script := `Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait("^c")
Start-Sleep -Milliseconds 100
[System.Windows.Forms.Clipboard]::GetText()`

	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return "", fmt.Errorf("powershell cursor text read: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
