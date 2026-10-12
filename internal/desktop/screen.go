package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"time"
)

// MaxScreenshots is the number of newest screenshot files retained in the
// capture directory. Older screenshots are pruned (best effort) after each
// successful capture so the screen-intelligence capture loop — which can run
// every 30s for weeks — cannot grow the directory without bound.
const MaxScreenshots = 200

// screenshotFilePattern matches exactly the file names Capture writes:
// screenshot-<unix>.png. Pruning only ever considers files matching this
// pattern, so unrelated files in the capture directory are never touched.
var screenshotFilePattern = regexp.MustCompile(`^screenshot-\d+\.png$`)

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

	// Bound the capture directory: nothing else ever deletes these files and
	// the screen-intel loop may run for weeks. Pruning is best effort and must
	// never fail or delay a capture, so errors are only logged.
	if deleted, perr := pruneScreenshots(s.outputDir, MaxScreenshots); perr != nil {
		slog.Warn("screenshot prune failed", "dir", s.outputDir, "error", perr)
	} else if deleted > 0 {
		slog.Debug("pruned old screenshots", "dir", s.outputDir, "deleted", deleted, "keep", MaxScreenshots)
	}

	return path, nil
}

// pruneScreenshots deletes the oldest screenshot files in dir until at most
// keep remain and returns how many files were deleted. Only regular files
// whose name matches screenshotFilePattern are considered: every other entry
// (subdirectories, unrelated files, names that merely look similar) is left
// untouched. A failed delete is logged and skipped rather than aborting the
// prune, so a capture is never affected by it.
func pruneScreenshots(dir string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	if keep < 0 {
		keep = 0
	}

	type screenshot struct {
		name    string
		modTime time.Time
	}
	shots := make([]screenshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !screenshotFilePattern.MatchString(entry.Name()) {
			continue
		}
		info, ierr := entry.Info()
		if ierr != nil {
			continue // raced with another prune; nothing to sort
		}
		shots = append(shots, screenshot{name: entry.Name(), modTime: info.ModTime()})
	}
	if len(shots) <= keep {
		return 0, nil
	}

	// Oldest first, with the name (which carries the capture unix timestamp) as
	// a deterministic tie-break for equal modification times.
	sort.Slice(shots, func(i, j int) bool {
		if shots[i].modTime.Equal(shots[j].modTime) {
			return shots[i].name < shots[j].name
		}
		return shots[i].modTime.Before(shots[j].modTime)
	})

	deleted := 0
	for _, shot := range shots[:len(shots)-keep] {
		if rerr := os.Remove(filepath.Join(dir, shot.name)); rerr != nil {
			slog.Warn("screenshot prune: delete failed", "file", shot.name, "error", rerr)
			continue
		}
		deleted++
	}
	return deleted, nil
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
