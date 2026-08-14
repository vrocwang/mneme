package desktop

import (
	"context"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/tools"
)

// VisionFunc takes a screenshot path and a target description, and returns
// the (x, y) pixel coordinates of the matching element, or an error.
type VisionFunc func(imagePath, target string) (int, int, error)

// CaptureGeometry records the spatial relationship between a screenshot and
// the screen, matching Rust's CaptureGeometry for Retina/multi-monitor safety.
type CaptureGeometry struct {
	RectX  int // window/screen top-left x in screen points
	RectY  int // window/screen top-left y in screen points
	RectW  int // window/screen width in points
	RectH  int // window/screen height in points
	ImgWPx int // screenshot width in pixels
	ImgHPx int // screenshot height in pixels
}

// VisionClick uses a vision model (LLM with image input) to locate UI
// elements on screen and click them.
type VisionClick struct {
	visionFunc VisionFunc
	screenCap  *ScreenCapture
}

// NewVisionClick creates a vision-based click helper.
func NewVisionClick(visionFunc VisionFunc) *VisionClick {
	captureDir := filepath.Join(config.TempDir(), "screenshots")
	os.MkdirAll(captureDir, 0755)
	return &VisionClick{
		visionFunc: visionFunc,
		screenCap:  NewScreenCapture(captureDir),
	}
}

// ClickByDescription takes a screenshot, asks the vision model to locate
// the element, maps pixel coords to screen coords, guards for the frontmost
// app, and clicks. Returns an error if the target app is not frontmost.
func (v *VisionClick) ClickByDescription(ctx context.Context, description string) error {
	if v.visionFunc == nil {
		return fmt.Errorf("vision_click: no vision model configured")
	}

	screenshotPath, geom, err := v.captureWithGeometry(ctx)
	if err != nil {
		return fmt.Errorf("vision_click: capture: %w", err)
	}
	defer os.Remove(screenshotPath)

	px, py, err := v.visionFunc(screenshotPath, description)
	if err != nil {
		return fmt.Errorf("vision_click: locate %q: %w", description, err)
	}

	// Map pixel coordinates to absolute screen coordinates.
	sx, sy := imageToScreen(geom, px, py)

	// Guard: refuse to click if the target is not the frontmost app.
	// Prevents clicking on the host application's own window.
	if err := guardFrontmostApp(description); err != nil {
		return err
	}

	cc := tools.NewComputerControl()
	result := cc.Execute(ctx, map[string]interface{}{
		"action": "mouse_click",
		"x":      float64(sx),
		"y":      float64(sy),
	})
	if result.Error != "" {
		return fmt.Errorf("vision_click: click at (%d,%d): %s", sx, sy, result.Error)
	}

	return nil
}

// LocateElement returns screen-mapped coordinates for an element.
func (v *VisionClick) LocateElement(ctx context.Context, description string) (int, int, error) {
	if v.visionFunc == nil {
		return 0, 0, fmt.Errorf("vision_click: no vision model configured")
	}

	screenshotPath, geom, err := v.captureWithGeometry(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("vision_click: screenshot: %w", err)
	}
	defer os.Remove(screenshotPath)

	px, py, err := v.visionFunc(screenshotPath, description)
	if err != nil {
		return 0, 0, err
	}
	sx, sy := imageToScreen(geom, px, py)
	return sx, sy, nil
}

// captureWithGeometry captures a screenshot and returns its geometry metadata.
func (v *VisionClick) captureWithGeometry(ctx context.Context) (path string, geom CaptureGeometry, err error) {
	path, err = v.screenCap.Capture(ctx)
	if err != nil || path == "" {
		return "", geom, fmt.Errorf("screenshot failed: %w", err)
	}

	geom, err = readCaptureGeometry(path)
	if err != nil {
		os.Remove(path)
		return "", geom, err
	}
	return path, geom, nil
}

// imageToScreen maps pixel coordinates from a screenshot to absolute screen
// coordinates using the capture geometry. Clamps result to the capture rect.
// Matching Rust's image_to_screen() in accessibility/vision_click.rs.
func imageToScreen(geom CaptureGeometry, px, py int) (int, int) {
	if geom.ImgWPx == 0 || geom.ImgHPx == 0 {
		return px, py // degenerate: no mapping available
	}
	sx := geom.RectX + (px*geom.RectW)/geom.ImgWPx
	sy := geom.RectY + (py*geom.RectH)/geom.ImgHPx
	// Clamp to window/screen rect so a wild model guess never lands elsewhere.
	if sx < geom.RectX {
		sx = geom.RectX
	}
	if sx > geom.RectX+geom.RectW {
		sx = geom.RectX + geom.RectW
	}
	if sy < geom.RectY {
		sy = geom.RectY
	}
	if sy > geom.RectY+geom.RectH {
		sy = geom.RectY + geom.RectH
	}
	return sx, sy
}

// readCaptureGeometry reads the screenshot's pixel dimensions and returns
// geometry assuming full-screen capture (rect = screen origin).
func readCaptureGeometry(path string) (CaptureGeometry, error) {
	f, err := os.Open(path)
	if err != nil {
		return CaptureGeometry{}, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return CaptureGeometry{}, fmt.Errorf("decode image config: %w", err)
	}
	geom := CaptureGeometry{
		RectX: 0, RectY: 0,
		RectW: cfg.Width, RectH: cfg.Height,
		ImgWPx: cfg.Width, ImgHPx: cfg.Height,
	}
	// On macOS, points != pixels on Retina displays. Try to get the point
	// resolution from system_profiler or sips.
	if runtime.GOOS == "darwin" {
		if w, h, ok := macScreenPoints(); ok {
			geom.RectW = w
			geom.RectH = h
		}
	}
	return geom, nil
}

func macScreenPoints() (int, int, bool) {
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").CombinedOutput()
	if err != nil {
		return 0, 0, false
	}
	// Parse "Resolution: 2560 x 1600" or "Resolution: 2560 x 1600 Retina"
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Resolution:") {
			line = strings.TrimPrefix(line, "Resolution:")
			line = strings.TrimSpace(line)
			// "2560 x 1600" or "2560 x 1600 Retina"
			line = strings.TrimSuffix(line, " Retina")
			line = strings.TrimSuffix(line, " (Retina)")
			parts := strings.Split(line, " x ")
			if len(parts) == 2 {
				w, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
				h, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				return w, h, w > 0 && h > 0
			}
		}
	}
	return 0, 0, false
}

// guardFrontmostApp refuses to click when the actual frontmost application is
// in the sensitive denylist. It checks the real frontmost app name, not the
// LLM-supplied description (which the model controls and could use to mask a
// sensitive target).
func guardFrontmostApp(_ string) error {
	appName := frontmostAppName()
	if appName == "" {
		return nil // can't determine, allow
	}
	denied, match := IsSensitiveApp(appName)
	if denied {
		return fmt.Errorf("vision_click: refusing — frontmost app %q matches denylist %q", appName, match)
	}
	return nil
}

func frontmostAppName() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("osascript", "-e",
			`tell application "System Events" to return name of first application process whose frontmost is true`,
		).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	case "windows":
		out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			`Add-Type -Name W -Namespace T -MemberDefinition '[DllImport("user32.dll")]public static extern IntPtr GetForegroundWindow();[DllImport("user32.dll")]public static extern int GetWindowText(IntPtr h, System.Text.StringBuilder s, int n);';$h=[T.W]::GetForegroundWindow();$s=New-Object System.Text.StringBuilder(256);[T.W]::GetWindowText($h,$s,256);$s.ToString()`,
		).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}
