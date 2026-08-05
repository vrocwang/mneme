// Screen Intelligence extension for Mneme.
//
// Provides screen awareness and OCR tools:
//   - screen_analyze: capture and describe screen content
//   - screen_ocr: extract text from screen via OCR
//   - screen_context: get active window and screen state
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	AgentDefs   []string `json:"agent_defs"`
	ProtocolMin int      `json:"protocol_min"`
}
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}
type callToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

var extManifest = manifest{
	Name:        "screen-intel",
	Version:     "0.1.0",
	Description: "Screen intelligence: analyze, OCR, context awareness",
	Tools:       []string{"screen_analyze", "screen_ocr", "screen_context"},
	AgentDefs:   []string{"screen_awareness_agent"},
	ProtocolMin: 1,
}

var agentDefs = []struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"systemPrompt"`
	ToolAllowlist []string `json:"toolAllowlist"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`
}{
	{
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
	},
}

var toolDefs = []toolDef{
	{
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
	},
	{
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
	},
	{
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
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("screen-intel extension starting")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		var req rpcRequest
		json.Unmarshal(line, &req)
		resp := handleRequest(&req)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		type lr struct{ Tools []toolDef }
		result, _ := json.Marshal(lr{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": agentDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "screen_analyze":
			result = screenAnalyze(ctx, params.Args)
		case "screen_ocr":
			result = screenOCR(ctx, params.Args)
		case "screen_context":
			result = screenContext(params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
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

func screenAnalyze(ctx context.Context, args map[string]interface{}) callToolResult {
	region := getStrArg(args, "region", "full")
	dir := getStrArg(args, "outputDir", "")
	if dir == "" {
		dir = screenshotDir()
	}
	os.MkdirAll(dir, 0755)

	path, err := captureScreen(ctx, region)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("capture: %v", err)}
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
	return callToolResult{Success: true, Output: string(b)}
}

func screenOCR(ctx context.Context, args map[string]interface{}) callToolResult {
	region := getStrArg(args, "region", "full")
	lang := getStrArg(args, "language", "eng")

	path, err := captureScreen(ctx, region)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("capture: %v", err)}
	}
	defer os.Remove(path)

	// Try tesseract
	if _, err := exec.LookPath("tesseract"); err != nil {
		return callToolResult{Error: "tesseract not found. Install: sudo apt install tesseract-ocr"}
	}

	outPath := path + ".txt"
	cmd := exec.CommandContext(ctx, "tesseract", path, path, "-l", lang)
	if out, err := cmd.CombinedOutput(); err != nil {
		return callToolResult{Error: fmt.Sprintf("OCR: %v (%s)", err, string(out))}
	}

	text, _ := os.ReadFile(outPath)
	os.Remove(outPath)

	return callToolResult{Success: true, Output: string(text)}
}

func screenContext(args map[string]interface{}) callToolResult {
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
	return callToolResult{Success: true, Output: string(b)}
}

func getStrArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}
