// Screen Intelligence extension for Mneme.
//
// Provides screen awareness and OCR tools:
//   - screen_analyze: capture and describe screen content
//   - screen_ocr: extract text from screen via OCR
//   - screen_context: get active window and screen state
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "screen-intel",
		Version:     "0.1.0",
		Description: "Screen intelligence: analyze, OCR, context awareness",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "screen_analyze",
		Description: "Capture the screen and describe its content. Returns the screenshot path and a textual description.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"region":    map[string]interface{}{"type": "string", "description": "Screen region: full, active_window, selection (default: full)"},
				"outputDir": map[string]interface{}{"type": "string", "description": "Directory to save screenshot (default: /tmp)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, screenAnalyze)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "screen_ocr",
		Description: "Extract text from the screen using OCR. Requires tesseract-ocr on Linux/macOS.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"region":   map[string]interface{}{"type": "string", "description": "Screen region: full, active_window (default: full)"},
				"language": map[string]interface{}{"type": "string", "description": "OCR language code (eng, chi_sim, etc. Default: eng)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, screenOCR)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "screen_context",
		Description: "Get the current screen context: active window, display info, and running applications",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"includeScreenshot": map[string]interface{}{"type": "boolean", "description": "Also capture a screenshot (default false)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return screenContext(args)
	})

	srv.RegisterAgent(extsdk.AgentDef{
		ID:          "screen_awareness_agent",
		Name:        "Screen Awareness",
		Description: "Captures and analyzes screen content using OCR and context extraction",
		Tier:        "worker",
		SystemPrompt: `You are a screen awareness specialist. Capture and analyze what's on the user's screen.
- Take screenshots of relevant areas
- Extract text via OCR when needed
- Identify active applications and windows
- Describe what you see clearly and concisely`,
		ToolAllowlist: []string{"screen_analyze", "screen_ocr", "screen_context", "read_file", "shell"},
		MaxIterations: 8,
		Hidden:        false,
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "screen-intel: %v\n", err)
		os.Exit(1)
	}
}

func screenshotDir() string {
	exe, _ := os.Executable()
	workspace := filepath.Join(filepath.Dir(exe), "data")
	dir := filepath.Join(workspace, "screenshots")
	os.MkdirAll(dir, 0755)
	return dir
}

var outputDir = screenshotDir()

func captureScreen(ctx context.Context, region string) (string, error) {
	os.MkdirAll(outputDir, 0755)
	path := filepath.Join(outputDir, fmt.Sprintf("screen-%d.png", time.Now().UnixMilli()))

	switch runtime.GOOS {
	case "darwin":
		args := []string{"-x", path}
		if region == "selection" {
			args = []string{"-s", path}
		}
		if region == "active_window" {
			args = []string{"-w", path}
		}
		return path, exec.CommandContext(ctx, "screencapture", args...).Run()
	case "linux":
		// Try gnome-screenshot, then import (ImageMagick)
		if _, err := exec.LookPath("gnome-screenshot"); err == nil {
			args := []string{"-f", path}
			if region == "active_window" {
				args = []string{"-w", "-f", path}
			}
			return path, exec.CommandContext(ctx, "gnome-screenshot", args...).Run()
		}
		return path, exec.CommandContext(ctx, "import", "-window", "root", path).Run()
	case "windows":
		ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$s = [System.Windows.Forms.Screen]::PrimaryScreen
$b = New-Object System.Drawing.Bitmap($s.Bounds.Width, $s.Bounds.Height)
$g = [System.Drawing.Graphics]::FromImage($b)
$g.CopyFromScreen(0,0,0,0,$b.Size)
$b.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose(); $b.Dispose()`, strings.ReplaceAll(path, "\\", "\\\\"))
		return path, exec.CommandContext(ctx, "powershell", "-Command", ps).Run()
	}
	return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
}

func getActiveWindow() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("osascript", "-e", `tell app "System Events" to name of first process whose frontmost is true`).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	case "linux":
		out, err := exec.Command("xdotool", "getactivewindow", "getwindowname").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	case "windows":
		out, err := exec.Command("powershell", "-Command", `Add-Type @"
using System; using System.Runtime.InteropServices; using System.Text;
public class W { [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
[DllImport("user32.dll")] public static extern int GetWindowText(IntPtr h, StringBuilder t, int c);
[DllImport("user32.dll")] public static extern int GetWindowTextLength(IntPtr h); }
"@; $h=[W]::GetForegroundWindow(); $l=[W]::GetWindowTextLength($h); $s=New-Object Text.StringBuilder($l+1); [W]::GetWindowText($h,$s,$l+1); $s.ToString()`).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return ""
}

func screenAnalyze(ctx context.Context, args map[string]interface{}) extsdk.Result {
	region := getStrArg(args, "region", "full")
	dir := getStrArg(args, "outputDir", "")
	if dir == "" {
		dir = screenshotDir()
	}
	os.MkdirAll(dir, 0755)

	path, err := captureScreen(ctx, region)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("capture: %v", err)}
	}

	activeWin := getActiveWindow()

	// Read screenshot for base64 encoding
	data, _ := os.ReadFile(path)
	b64 := ""
	if len(data) > 0 {
		b64 = base64.StdEncoding.EncodeToString(data)
	}

	type result struct {
		ScreenshotPath string `json:"screenshot_path"`
		ActiveWindow   string `json:"active_window"`
		Region         string `json:"region"`
		SizeBytes      int    `json:"size_bytes"`
		Base64         string `json:"base64,omitempty"`
	}
	r := result{
		ScreenshotPath: path,
		ActiveWindow:   activeWin,
		Region:         region,
		SizeBytes:      len(data),
	}
	if len(b64) < 500000 {
		r.Base64 = b64
	}

	b, _ := json.MarshalIndent(r, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func screenOCR(ctx context.Context, args map[string]interface{}) extsdk.Result {
	region := getStrArg(args, "region", "full")
	lang := getStrArg(args, "language", "eng")

	path, err := captureScreen(ctx, region)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("capture: %v", err)}
	}
	defer os.Remove(path)

	// Try tesseract
	if _, err := exec.LookPath("tesseract"); err != nil {
		return extsdk.Result{Error: "tesseract not found. Install: sudo apt install tesseract-ocr"}
	}

	outPath := path + ".txt"
	cmd := exec.CommandContext(ctx, "tesseract", path, path, "-l", lang)
	if out, err := cmd.CombinedOutput(); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("OCR: %v (%s)", err, string(out))}
	}

	text, _ := os.ReadFile(outPath)
	os.Remove(outPath)

	return extsdk.Result{Success: true, Output: string(text)}
}

func screenContext(args map[string]interface{}) extsdk.Result {
	includeScreen, _ := args["includeScreenshot"].(bool)

	ctx := map[string]interface{}{
		"active_window": getActiveWindow(),
		"platform":      runtime.GOOS,
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	if includeScreen {
		path, err := captureScreen(context.Background(), "full")
		if err == nil {
			ctx["screenshot_path"] = path
		}
	}

	b, _ := json.MarshalIndent(ctx, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func getStrArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}
