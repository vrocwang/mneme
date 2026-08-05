package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// ScreenCapture captures screenshots.
type ScreenCapture struct {
	outputDir string
}

// NewScreenCapture creates a screen capture tool.
// On first call, logs warnings for missing platform-specific automation tools.
func NewScreenCapture(outputDir string) *ScreenCapture {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		slog.Warn("screen capture output directory creation failed", "dir", outputDir, "error", err)
	}
	checkDepsOnce.Do(func() { checkPlatformDeps() })
	return &ScreenCapture{outputDir: outputDir}
}

// Capture takes a screenshot and returns the file path.
func (s *ScreenCapture) Capture(ctx context.Context) (string, error) {
	filename := fmt.Sprintf("screenshot-%d.png", time.Now().Unix())
	path := filepath.Join(s.outputDir, filename)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "screencapture", "-x", path)
	case "linux":
		cmd = exec.CommandContext(ctx, "import", "-window", "root", path)
	case "windows":
		ps := fmt.Sprintf(`
			Add-Type -AssemblyName System.Windows.Forms
			$screen = [System.Windows.Forms.Screen]::PrimaryScreen
			$bmp = New-Object System.Drawing.Bitmap($screen.Bounds.Width, $screen.Bounds.Height)
			$gfx = [System.Drawing.Graphics]::FromImage($bmp)
			$gfx.CopyFromScreen(0, 0, 0, 0, $bmp.Size)
			$bmp.Save('%s')
			$gfx.Dispose()
			$bmp.Dispose()
		`, path)
		cmd = exec.CommandContext(ctx, "powershell", "-Command", ps)
	}

	if cmd == nil {
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("screencapture: %w\n%s", err, output)
	}

	return path, nil
}

// ScreenContext contains info about what's on screen.
type ScreenContext struct {
	ScreenshotPath string `json:"screenshotPath"`
	ActiveWindow   string `json:"activeWindow,omitempty"`
	Description    string `json:"description,omitempty"` // filled by vision model
}

// GetScreenContext captures screen and active window info.
func (s *ScreenCapture) GetScreenContext(ctx context.Context) (*ScreenContext, error) {
	path, err := s.Capture(ctx)
	if err != nil {
		return nil, err
	}

	sc := &ScreenContext{
		ScreenshotPath: path,
		ActiveWindow:   getActiveWindow(),
	}
	return sc, nil
}

func getActiveWindow() string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("osascript", "-e",
			`tell application "System Events" to get name of first application process whose frontmost is true`)
	case "linux":
		cmd = exec.Command("xdotool", "getactivewindow", "getwindowname")
	case "windows":
		cmd = exec.Command("powershell", "-Command",
			`Add-Type @"
using System;
using System.Runtime.InteropServices;
using System.Text;
public class WinAPI {
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern int GetWindowText(IntPtr hWnd, StringBuilder text, int count);
    [DllImport("user32.dll")] public static extern int GetWindowTextLength(IntPtr hWnd);
}
"@
$hwnd = [WinAPI]::GetForegroundWindow()
$len = [WinAPI]::GetWindowTextLength($hwnd)
$sb = New-Object System.Text.StringBuilder($len + 1)
[WinAPI]::GetWindowText($hwnd, $sb, $len + 1)
$sb.ToString()`)
	}

	if cmd != nil {
		output, _ := cmd.Output()
		return string(output)
	}
	return ""
}
