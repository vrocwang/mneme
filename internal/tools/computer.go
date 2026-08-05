package tools

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ComputerControl provides keyboard and mouse automation across platforms.
//
// Platform dependencies:
//
//	macOS   — cliclick (brew install cliclick) for mouse clicks; falls back
//	          to Python Quartz for cursor positioning.
//	Linux   — ydotool (Wayland) or xdotool (X11); both support mouse and
//	          keyboard input. ydotool is preferred when available.
//	          Query operations (mouse position, screen size) require xdotool.
//	Windows — PowerShell + Win32 API (built-in, no extra deps).
//
// Mouse clicks on macOS will fail if cliclick is not installed.
// Keyboard and cursor positioning work without it.
type ComputerControl struct{}

// NewComputerControl creates a computer control tool.
func NewComputerControl() *ComputerControl {
	return &ComputerControl{}
}

func (t *ComputerControl) Schema() Schema {
	return Schema{
		Name:        "computer",
		Description: "Control mouse and keyboard: move mouse, click, type text, press keys, take screenshots of specific regions. Use for GUI automation when accessibility APIs are unavailable.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{
						"mouse_move", "mouse_click", "mouse_double_click", "mouse_right_click",
						"mouse_drag", "type_text", "key_press", "key_combo",
						"mouse_scroll", "mouse_position", "screen_size",
					},
				},
				"x":       map[string]interface{}{"type": "integer", "description": "X coordinate"},
				"y":       map[string]interface{}{"type": "integer", "description": "Y coordinate"},
				"start_x": map[string]interface{}{"type": "integer", "description": "Start X for mouse_drag"},
				"start_y": map[string]interface{}{"type": "integer", "description": "Start Y for mouse_drag"},
				"text":    map[string]interface{}{"type": "string", "description": "Text to type (for type_text action)"},
				"keys":    map[string]interface{}{"type": "string", "description": "Key or key combination (e.g. 'Return', 'Escape', 'ctrl+c', 'alt+Tab')"},
				"button":  map[string]interface{}{"type": "string", "enum": []string{"left", "middle", "right"}, "description": "Mouse button. Default: left"},
				"amount":  map[string]interface{}{"type": "integer", "description": "Scroll amount: positive = up, negative = down. Default: 1"},
			},
			"required": []string{"action"},
		},
	}
}

func (t *ComputerControl) Execute(ctx context.Context, args map[string]interface{}) Result {
	action, _ := args["action"].(string)
	if action == "" {
		return Result{Error: "action is required"}
	}
	switch action {
	case "mouse_position":
		return t.mousePosition()
	case "screen_size":
		return t.screenSize()
	case "mouse_move":
		x, err := requireCoord(args, "x")
		if err != nil {
			return Result{Error: err.Error()}
		}
		y, err := requireCoord(args, "y")
		if err != nil {
			return Result{Error: err.Error()}
		}
		return t.runMouseMove(ctx, x, y)
	case "mouse_click":
		x, err := requireCoord(args, "x")
		if err != nil {
			return Result{Error: err.Error()}
		}
		y, err := requireCoord(args, "y")
		if err != nil {
			return Result{Error: err.Error()}
		}
		return t.runMouseClick(ctx, x, y, buttonArg(args))
	case "mouse_double_click":
		x, err := requireCoord(args, "x")
		if err != nil {
			return Result{Error: err.Error()}
		}
		y, err := requireCoord(args, "y")
		if err != nil {
			return Result{Error: err.Error()}
		}
		return t.runMouseDoubleClick(ctx, x, y)
	case "mouse_right_click":
		x, err := requireCoord(args, "x")
		if err != nil {
			return Result{Error: err.Error()}
		}
		y, err := requireCoord(args, "y")
		if err != nil {
			return Result{Error: err.Error()}
		}
		return t.runMouseRightClick(ctx, x, y)
	case "mouse_drag":
		sx, err := requireCoord(args, "start_x")
		if err != nil {
			return Result{Error: err.Error()}
		}
		sy, err := requireCoord(args, "start_y")
		if err != nil {
			return Result{Error: err.Error()}
		}
		ex, err := requireCoord(args, "x")
		if err != nil {
			return Result{Error: err.Error()}
		}
		ey, err := requireCoord(args, "y")
		if err != nil {
			return Result{Error: err.Error()}
		}
		return t.runMouseDrag(ctx, sx, sy, ex, ey)
	case "mouse_scroll":
		return t.runMouseScroll(ctx, toIntWithDefault(args, "amount", 1))
	case "type_text":
		text, _ := args["text"].(string)
		return t.runTypeText(ctx, text)
	case "key_press":
		keys, _ := args["keys"].(string)
		return t.runKeyPress(ctx, keys)
	case "key_combo":
		keys, _ := args["keys"].(string)
		return t.runKeyCombo(ctx, keys)
	default:
		return Result{Error: fmt.Sprintf("unknown action: %s", action)}
	}
}

func (t *ComputerControl) PermissionLevel() PermissionLevel { return PermExecute }
func (t *ComputerControl) SideEffects() bool                { return true }
func (t *ComputerControl) ConcurrencySafe() bool            { return false }
func (t *ComputerControl) MaxResultChars() int              { return 2000 }

// ── Windows C# + P/Invoke helper (shared PowerShell fragment) ─────────
// All Windows mouse/keyboard functions use a single Add-Type block that
// declares the Win32 imports once, then invokes the requested operation.

const winPInvokeHelper = `
Add-Type -Name Win32 -Namespace OH -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
[DllImport("user32.dll")] public static extern bool GetCursorPos(out POINT pt);
[DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, System.IntPtr dwExtraInfo);
[DllImport("user32.dll")] public static extern void keybd_event(byte bVk, byte bScan, uint dwFlags, System.IntPtr dwExtraInfo);
[DllImport("user32.dll")] public static extern short GetAsyncKeyState(int vKey);
public struct POINT { public int x; public int y; }
public const uint MOUSEEVENTF_LEFTDOWN = 0x0002;
public const uint MOUSEEVENTF_LEFTUP = 0x0004;
public const uint MOUSEEVENTF_RIGHTDOWN = 0x0008;
public const uint MOUSEEVENTF_RIGHTUP = 0x0010;
public const uint MOUSEEVENTF_MIDDLEDOWN = 0x0020;
public const uint MOUSEEVENTF_MIDDLEUP = 0x0040;
public const uint MOUSEEVENTF_WHEEL = 0x0800;
public const uint MOUSEEVENTF_ABSOLUTE = 0x8000;
public const uint KEYEVENTF_KEYUP = 0x0002;
public const int WHEEL_DELTA = 120;
'@
`

// winPS runs a PowerShell command and returns stdout or error.
func winPS(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// winKeyCode maps key names to Windows virtual-key codes (case-insensitive).
func winKeyCode(key string) (int, bool) {
	m := map[string]int{
		"Return": 0x0D, "Enter": 0x0D, "Escape": 0x1B, "Esc": 0x1B,
		"Tab": 0x09, "Space": 0x20, "Backspace": 0x08, "Delete": 0x2E,
		"Left": 0x25, "Right": 0x27, "Down": 0x28, "Up": 0x26,
		"Home": 0x24, "End": 0x23, "PageUp": 0x21, "PageDown": 0x22,
		"F1": 0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73, "F5": 0x74, "F6": 0x75,
		"F7": 0x76, "F8": 0x77, "F9": 0x78, "F10": 0x79, "F11": 0x7A, "F12": 0x7B,
		"PrintScreen": 0x2C, "Insert": 0x2D, "CapsLock": 0x14, "NumLock": 0x90,
	}
	// Normalize: "return" → "Return", "esc" → "Escape", etc.
	code, ok := m[normalizeKeyName(key)]
	if !ok {
		code, ok = m[key] // fallback exact match
	}
	return code, ok
}

// normalizeKeyName capitalizes the first letter for consistent lookup.
func normalizeKeyName(key string) string {
	if len(key) == 0 {
		return key
	}
	// Special case: "escape" → "Escape", "esc" → "Esc"
	lower := strings.ToLower(key)
	special := map[string]string{
		"escape": "Escape", "backspace": "Backspace", "capslock": "CapsLock",
		"numlock": "NumLock", "printscreen": "PrintScreen", "pageup": "PageUp",
		"pagedown": "PageDown",
	}
	if s, ok := special[lower]; ok {
		return s
	}
	if len(key) == 1 {
		return key
	}
	// Capitalize first letter: "return" → "Return"
	return strings.ToUpper(key[:1]) + strings.ToLower(key[1:])
}

// winModKeyCode maps modifier names to Windows VK codes.
func winModKeyCode(mod string) (int, bool) {
	m := map[string]int{
		"ctrl": 0xA2, "control": 0xA2,
		"alt": 0xA4, "menu": 0xA4,
		"shift": 0xA0,
		"win":   0x5B, "windows": 0x5B, "cmd": 0x5B, "super": 0x5B,
	}
	code, ok := m[strings.ToLower(mod)]
	return code, ok
}

// winSendKey maps key names to SendKeys syntax.
func winSendKey(key string) string {
	m := map[string]string{
		"Return": "{ENTER}", "Enter": "{ENTER}", "Escape": "{ESC}", "Esc": "{ESC}",
		"Tab": "{TAB}", "Backspace": "{BACKSPACE}", "Delete": "{DELETE}",
		"Left": "{LEFT}", "Right": "{RIGHT}", "Down": "{DOWN}", "Up": "{UP}",
		"Home": "{HOME}", "End": "{END}", "PageUp": "{PGUP}", "PageDown": "{PGDN}",
		"F1": "{F1}", "F2": "{F2}", "F3": "{F3}", "F4": "{F4}", "F5": "{F5}", "F6": "{F6}",
		"F7": "{F7}", "F8": "{F8}", "F9": "{F9}", "F10": "{F10}", "F11": "{F11}", "F12": "{F12}",
		"Insert": "{INSERT}", "PrintScreen": "{PRTSC}",
		"Space": " ", "+": "{+}", "^": "{^}", "%": "{%}", "~": "{~}",
		"(": "{(}", ")": "{)}", "[": "{[}", "]": "{]}", "{": "{{}", "}": "{}}",
	}
	if s, ok := m[key]; ok {
		return s
	}
	if len(key) == 1 {
		return key
	}
	return "{" + key + "}"
}

// ── mousePosition ─────────────────────────────────────────────────────

func (t *ComputerControl) mousePosition() Result {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("xdotool", "getmouselocation").CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("xdotool: %v — %s", err, out)}
		}
		return Result{Success: true, Output: string(out)}
	case "darwin":
		out, err := exec.Command("osascript", "-e", `tell application "System Events" to get position of mouse`).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("osascript: %v", err)}
		}
		return Result{Success: true, Output: string(out)}
	case "windows":
		script := winPInvokeHelper + `
$pt = New-Object OH.Win32+POINT
[OH.Win32]::GetCursorPos([ref]$pt) | Out-Null
Write-Host "$($pt.x) $($pt.y)"
`
		out, err := winPS(context.Background(), script)
		if err != nil {
			return Result{Error: fmt.Sprintf("GetCursorPos: %v", err)}
		}
		parts := strings.Fields(out)
		if len(parts) >= 2 {
			return Result{Success: true, Output: fmt.Sprintf("x:%s y:%s", parts[0], parts[1])}
		}
		return Result{Success: true, Output: out}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── screenSize ────────────────────────────────────────────────────────

func (t *ComputerControl) screenSize() Result {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("xdotool", "getdisplaygeometry").CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("xdotool: %v", err)}
		}
		return Result{Success: true, Output: string(out)}
	case "darwin":
		out, err := exec.Command("osascript", "-e", `tell application "Finder" to get bounds of window of desktop`).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("osascript: %v", err)}
		}
		return Result{Success: true, Output: string(out)}
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms
$s = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
Write-Host "$($s.Width)x$($s.Height)"
`
		out, err := winPS(context.Background(), script)
		if err != nil {
			return Result{Error: fmt.Sprintf("Screen size: %v", err)}
		}
		return Result{Success: true, Output: out}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── mouseMove ──────────────────────────────────────────────────────────

func (t *ComputerControl) runMouseMove(ctx context.Context, x, y int) Result {
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux mouse control"}
		}
		out, err := exec.CommandContext(ctx, tool, linuxMoveArgs(tool, x, y)...).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s mousemove: %v — %s", tool, err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Mouse moved to (%d, %d)", x, y)}
	case "darwin":
		script := fmt.Sprintf(`tell application "System Events" to set position of mouse to {%d, %d}`, x, y)
		out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("osascript: %v — %s", err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Mouse moved to (%d, %d)", x, y)}
	case "windows":
		script := winPInvokeHelper + fmt.Sprintf("[OH.Win32]::SetCursorPos(%d, %d) | Out-Null; Write-Host 'ok'", x, y)
		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("SetCursorPos: %v", err)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Mouse moved to (%d, %d)", x, y)}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── mouseClick ─────────────────────────────────────────────────────────

func (t *ComputerControl) runMouseClick(ctx context.Context, x, y int, button string) Result {
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux mouse control"}
		}
		btnCode := "1"
		if button == "right" {
			btnCode = "3"
		} else if button == "middle" {
			btnCode = "2"
		}
		out, err := exec.CommandContext(ctx, tool, linuxClickArgs(tool, x, y, btnCode)...).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s click: %v — %s", tool, err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Clicked (%d, %d) [%s]", x, y, button)}
	case "darwin":
		// Prefer cliclick; fall back to Python Quartz (built-in on macOS).
		if _, err := exec.LookPath("cliclick"); err == nil {
			action := fmt.Sprintf("c:%d,%d", x, y)
			if button == "right" {
				action = fmt.Sprintf("rc:%d,%d", x, y)
			} else if button == "middle" {
				action = fmt.Sprintf("mc:%d,%d", x, y)
			}
			_, err := exec.CommandContext(ctx, "cliclick", action).CombinedOutput()
			if err != nil {
				return Result{Error: fmt.Sprintf("cliclick: %v", err)}
			}
			return Result{Success: true, Output: fmt.Sprintf("Clicked (%d, %d) [%s]", x, y, button)}
		}
		// Fallback: Python Quartz (Core Graphics, no external deps on macOS).
		if err := macQuartzClick(ctx, x, y, button); err == nil {
			return Result{Success: true, Output: fmt.Sprintf("Clicked (%d, %d) [%s]", x, y, button)}
		}
		return Result{Error: "computer control: install cliclick (brew install cliclick) or ensure Python3 with pyobjc is available for macOS mouse control"}
	case "windows":
		downFlag, upFlag := winMouseButtonFlags(button)
		script := winPInvokeHelper + fmt.Sprintf(`
[OH.Win32]::SetCursorPos(%d, %d) | Out-Null
[OH.Win32]::mouse_event(%d, 0, 0, 0, [System.IntPtr]::Zero)
Start-Sleep -Milliseconds 50
[OH.Win32]::mouse_event(%d, 0, 0, 0, [System.IntPtr]::Zero)
Write-Host 'ok'
`, x, y, downFlag, upFlag)
		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("mouse_event: %v", err)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Clicked (%d, %d) [%s]", x, y, button)}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── mouseDoubleClick ───────────────────────────────────────────────────

func (t *ComputerControl) runMouseDoubleClick(ctx context.Context, x, y int) Result {
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux mouse control"}
		}
		out, err := exec.CommandContext(ctx, tool, linuxDblClickArgs(tool, x, y)...).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s double-click: %v — %s", tool, err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Double-clicked (%d, %d)", x, y)}
	case "darwin":
		if _, err := exec.LookPath("cliclick"); err == nil {
			_, err := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("dc:%d,%d", x, y)).CombinedOutput()
			if err != nil {
				return Result{Error: fmt.Sprintf("cliclick: %v", err)}
			}
			return Result{Success: true, Output: fmt.Sprintf("Double-clicked (%d, %d)", x, y)}
		}
		if err := macQuartzClick(ctx, x, y, "left"); err != nil {
			return Result{Error: fmt.Sprintf("quartz click: %v", err)}
		}
		// 80ms between clicks matches the Windows double-click timing
		time.Sleep(80 * time.Millisecond)
		if err := macQuartzClick(ctx, x, y, "left"); err != nil {
			return Result{Error: fmt.Sprintf("quartz click 2: %v", err)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Double-clicked (%d, %d)", x, y)}
	case "windows":
		script := winPInvokeHelper + fmt.Sprintf(`
[OH.Win32]::SetCursorPos(%d, %d) | Out-Null
$down = [OH.Win32]::MOUSEEVENTF_LEFTDOWN
$up = [OH.Win32]::MOUSEEVENTF_LEFTUP
for ($i = 0; $i -lt 2; $i++) {
	[OH.Win32]::mouse_event($down, 0, 0, 0, [System.IntPtr]::Zero)
	Start-Sleep -Milliseconds 40
	[OH.Win32]::mouse_event($up, 0, 0, 0, [System.IntPtr]::Zero)
	Start-Sleep -Milliseconds 80
}
Write-Host 'ok'
`, x, y)
		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("double-click: %v", err)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Double-clicked (%d, %d)", x, y)}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── mouseRightClick ────────────────────────────────────────────────────

func (t *ComputerControl) runMouseRightClick(ctx context.Context, x, y int) Result {
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux mouse control"}
		}
		out, err := exec.CommandContext(ctx, tool, linuxClickArgs(tool, x, y, "3")...).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s right-click: %v — %s", tool, err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Right-clicked (%d, %d)", x, y)}
	case "darwin":
		if _, err := exec.LookPath("cliclick"); err == nil {
			_, err := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("rc:%d,%d", x, y)).CombinedOutput()
			if err != nil {
				return Result{Error: fmt.Sprintf("cliclick: %v", err)}
			}
			return Result{Success: true, Output: fmt.Sprintf("Right-clicked (%d, %d)", x, y)}
		}
		if err := macQuartzClick(ctx, x, y, "right"); err == nil {
			return Result{Success: true, Output: fmt.Sprintf("Right-clicked (%d, %d)", x, y)}
		}
		return Result{Error: "computer control: install cliclick for macOS"}
	case "windows":
		script := winPInvokeHelper + fmt.Sprintf(`
[OH.Win32]::SetCursorPos(%d, %d) | Out-Null
[OH.Win32]::mouse_event([OH.Win32]::MOUSEEVENTF_RIGHTDOWN, 0, 0, 0, [System.IntPtr]::Zero)
Start-Sleep -Milliseconds 40
[OH.Win32]::mouse_event([OH.Win32]::MOUSEEVENTF_RIGHTUP, 0, 0, 0, [System.IntPtr]::Zero)
Write-Host 'ok'
`, x, y)
		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("right-click: %v", err)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Right-clicked (%d, %d)", x, y)}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── mouseDrag ──────────────────────────────────────────────────────────

func (t *ComputerControl) runMouseDrag(ctx context.Context, sx, sy, ex, ey int) Result {
	released := false
	defer func() {
		if !released {
			switch runtime.GOOS {
			case "linux":
				if t := linuxInputTool(); t != "" {
					exec.Command(t, "mouseup", "1").Run()
				}
			case "darwin":
				exec.Command("osascript", "-e", `tell application "System Events" to click (button 1 of mouse) up`).Run()
			case "windows":
				exec.Command("powershell", "-Command",
					`Add-Type -Name W -Namespace T -Member '[DllImport("user32.dll")]public static extern void mouse_event(int f,int x,int y,int d,int e);';[T.W]::mouse_event(4,0,0,0,0)`).Run()
			}
		}
	}()
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			released = true
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux mouse control"}
		}
		var args []string
		if tool == "ydotool" {
			args = []string{"mousemove", "--absolute", fmt.Sprint(sx), fmt.Sprint(sy),
				"mousedown", "1",
				"mousemove", "--absolute", fmt.Sprint(ex), fmt.Sprint(ey),
				"mouseup", "1"}
		} else {
			args = []string{"mousemove", fmt.Sprint(sx), fmt.Sprint(sy),
				"mousedown", "1",
				"mousemove", fmt.Sprint(ex), fmt.Sprint(ey),
				"mouseup", "1"}
		}
		out, err := exec.CommandContext(ctx, tool, args...).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s drag: %v — %s", tool, err, out)}
		}
		released = true
		return Result{Success: true, Output: fmt.Sprintf("Dragged from (%d,%d) to (%d,%d)", sx, sy, ex, ey)}
	case "darwin":
		if _, err := exec.LookPath("cliclick"); err == nil {
			_, err := exec.CommandContext(ctx, "cliclick", fmt.Sprintf("dd:%d,%d", sx, sy), fmt.Sprintf("du:%d,%d", ex, ey)).CombinedOutput()
			if err != nil {
				return Result{Error: fmt.Sprintf("cliclick: %v", err)}
			}
			released = true
			return Result{Success: true, Output: fmt.Sprintf("Dragged from (%d,%d) to (%d,%d)", sx, sy, ex, ey)}
		}
		return Result{Error: "computer control: install cliclick for macOS"}
	case "windows":
		script := winPInvokeHelper + fmt.Sprintf(`
[OH.Win32]::SetCursorPos(%d, %d) | Out-Null
[OH.Win32]::mouse_event([OH.Win32]::MOUSEEVENTF_LEFTDOWN, 0, 0, 0, [System.IntPtr]::Zero)
Start-Sleep -Milliseconds 30
[OH.Win32]::SetCursorPos(%d, %d) | Out-Null
Start-Sleep -Milliseconds 30
[OH.Win32]::mouse_event([OH.Win32]::MOUSEEVENTF_LEFTUP, 0, 0, 0, [System.IntPtr]::Zero)
Write-Host 'ok'
`, sx, sy, ex, ey)
		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("drag: %v", err)}
		}
		released = true
		return Result{Success: true, Output: fmt.Sprintf("Dragged from (%d,%d) to (%d,%d)", sx, sy, ex, ey)}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── mouseScroll ────────────────────────────────────────────────────────

func (t *ComputerControl) runMouseScroll(ctx context.Context, amount int) Result {
	switch runtime.GOOS {
	case "linux":
		btn := "4"
		abs := amount
		if amount < 0 {
			btn = "5"
			if amount == math.MinInt {
				abs = math.MaxInt
			} else {
				abs = -amount
			}
		}
		args := make([]string, 0, abs*2)
		for i := 0; i < abs; i++ {
			args = append(args, "click", btn)
		}
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux mouse control"}
		}
		out, err := exec.CommandContext(ctx, tool, args...).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s scroll: %v — %s", tool, err, out)}
		}
		dir := "up"
		if amount < 0 {
			dir = "down"
		}
		return Result{Success: true, Output: fmt.Sprintf("Scrolled %s %d steps", dir, abs)}
	case "darwin":
		if _, err := exec.LookPath("cliclick"); err == nil {
			abs := amount
			if abs < 0 {
				if abs == math.MinInt {
					abs = math.MaxInt
				} else {
					abs = -abs
				}
			}
			dir := "w"
			if amount < 0 {
				dir = "W"
			}
			args := make([]string, 0, abs)
			for i := 0; i < abs; i++ {
				args = append(args, dir+":100")
			}
			_, err := exec.CommandContext(ctx, "cliclick", args...).CombinedOutput()
			if err != nil {
				return Result{Error: fmt.Sprintf("cliclick scroll: %v", err)}
			}
			dirstr := "up"
			if amount < 0 {
				dirstr = "down"
			}
			return Result{Success: true, Output: fmt.Sprintf("Scrolled %s %d steps", dirstr, abs)}
		}
		return Result{Error: "computer control: install cliclick for macOS scroll"}
	case "windows":
		abs := amount
		if abs < 0 {
			if abs == math.MinInt {
				abs = math.MaxInt
			} else {
				abs = -abs
			}
		}
		delta := amount * 120 // WHEEL_DELTA
		script := winPInvokeHelper + fmt.Sprintf(`
for ($i = 0; $i -lt %d; $i++) {
	[OH.Win32]::mouse_event([OH.Win32]::MOUSEEVENTF_WHEEL, 0, 0, %d, [System.IntPtr]::Zero)
	Start-Sleep -Milliseconds 20
}
Write-Host 'ok'
`, abs, delta)
		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("scroll: %v", err)}
		}
		dir := "up"
		if amount < 0 {
			dir = "down"
		}
		return Result{Success: true, Output: fmt.Sprintf("Scrolled %s %d steps", dir, abs)}
	default:
		return Result{Error: "computer control: scroll not supported on this platform"}
	}
}

// ── typeText ───────────────────────────────────────────────────────────

func (t *ComputerControl) runTypeText(ctx context.Context, text string) Result {
	if text == "" {
		return Result{Error: "text is required for type_text"}
	}
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux keyboard control"}
		}
		out, err := exec.CommandContext(ctx, tool, "type", text).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s type: %v — %s", tool, err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Typed %d characters", len(text))}
	case "darwin":
		script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escapeAppleScript(text))
		out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("osascript keystroke: %v — %s", err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Typed %d characters", len(text))}
	case "windows":
		escaped := winEscapeForSendKeys(winEscapeSendKeysSpecials(text))
		script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('%s'); Write-Host 'ok'`, escaped)
		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("SendKeys: %v", err)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Typed %d characters", len(text))}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── keyPress ───────────────────────────────────────────────────────────

func (t *ComputerControl) runKeyPress(ctx context.Context, keys string) Result {
	if keys == "" {
		return Result{Error: "keys is required for key_press"}
	}
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux keyboard control"}
		}
		out, err := exec.CommandContext(ctx, tool, "key", keys).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s key: %v — %s", tool, err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Pressed key: %s", keys)}
	case "darwin":
		var script string
		if code, ok := appleScriptKeyCode(keys); ok {
			script = fmt.Sprintf(`tell application "System Events" to key code %s`, code)
		} else {
			script = fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escapeAppleScript(keys))
		}
		out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("osascript key: %v — %s", err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Pressed key: %s", keys)}
	case "windows":
		// Use SendKeys for simple keys, keybd_event for special keys with VK codes
		if vk, ok := winKeyCode(keys); ok {
			script := winPInvokeHelper + fmt.Sprintf(`
[OH.Win32]::keybd_event(%d, 0, 0, [System.IntPtr]::Zero)
Start-Sleep -Milliseconds 30
[OH.Win32]::keybd_event(%d, 0, [OH.Win32]::KEYEVENTF_KEYUP, [System.IntPtr]::Zero)
Write-Host 'ok'
`, vk, vk)
			if _, err := winPS(ctx, script); err != nil {
				return Result{Error: fmt.Sprintf("keybd_event: %v", err)}
			}
		} else {
			sk := winSendKey(keys)
			script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('%s'); Write-Host 'ok'`, winEscapeForSendKeys(sk))
			if _, err := winPS(ctx, script); err != nil {
				return Result{Error: fmt.Sprintf("SendKeys: %v", err)}
			}
		}
		return Result{Success: true, Output: fmt.Sprintf("Pressed key: %s", keys)}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── keyCombo ───────────────────────────────────────────────────────────

func (t *ComputerControl) runKeyCombo(ctx context.Context, keys string) Result {
	if keys == "" {
		return Result{Error: "keys is required for key_combo"}
	}
	switch runtime.GOOS {
	case "linux":
		tool := linuxInputTool()
		if tool == "" {
			return Result{Error: "computer control: install xdotool (X11) or ydotool (Wayland) for Linux keyboard control"}
		}
		out, err := exec.CommandContext(ctx, tool, "key", keys).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("%s key combo: %v — %s", tool, err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Pressed combo: %s", keys)}
	case "darwin":
		parts := strings.Split(keys, "+")
		last := strings.TrimSpace(parts[len(parts)-1])
		mods := strings.Join(parts[:len(parts)-1], "+")
		modStr := keysToAppleScriptModifiers(mods)
		script := fmt.Sprintf(`tell application "System Events" to keystroke "%s" using {%s}`, escapeAppleScript(last), modStr)
		out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
		if err != nil {
			return Result{Error: fmt.Sprintf("osascript key combo: %v — %s", err, out)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Pressed combo: %s", keys)}
	case "windows":
		parts := strings.Split(keys, "+")
		var modScript strings.Builder
		var modVKs []int
		lastKey := strings.TrimSpace(parts[len(parts)-1])
		// Press modifier keys down
		for i := 0; i < len(parts)-1; i++ {
			vk, ok := winModKeyCode(strings.TrimSpace(parts[i]))
			if !ok {
				continue
			}
			modVKs = append(modVKs, vk)
			modScript.WriteString(fmt.Sprintf("[OH.Win32]::keybd_event(%d, 0, 0, [System.IntPtr]::Zero)\n", vk))
		}
		// Press and release main key
		mainVK, mainIsVK := winKeyCode(lastKey)
		// Release modifiers in reverse order
		var releaseScript strings.Builder
		for i := len(modVKs) - 1; i >= 0; i-- {
			releaseScript.WriteString(fmt.Sprintf("[OH.Win32]::keybd_event(%d, 0, [OH.Win32]::KEYEVENTF_KEYUP, [System.IntPtr]::Zero)\n", modVKs[i]))
		}

		script := winPInvokeHelper + modScript.String()
		script += "Start-Sleep -Milliseconds 20\n"
		if mainIsVK {
			script += fmt.Sprintf("[OH.Win32]::keybd_event(%d, 0, 0, [System.IntPtr]::Zero)\n", mainVK)
			script += "Start-Sleep -Milliseconds 30\n"
			script += fmt.Sprintf("[OH.Win32]::keybd_event(%d, 0, [OH.Win32]::KEYEVENTF_KEYUP, [System.IntPtr]::Zero)\n", mainVK)
		} else {
			sk := winSendKey(lastKey)
			script += fmt.Sprintf("Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('%s')\n", winEscapeForSendKeys(sk))
		}
		script += releaseScript.String() + "\nWrite-Host 'ok'"

		if _, err := winPS(ctx, script); err != nil {
			return Result{Error: fmt.Sprintf("keybd_event combo: %v", err)}
		}
		return Result{Success: true, Output: fmt.Sprintf("Pressed combo: %s", keys)}
	default:
		return Result{Error: "computer control: unsupported platform"}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────

func buttonArg(args map[string]interface{}) string {
	b, _ := args["button"].(string)
	if b == "" {
		return "left"
	}
	return b
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}

func toIntWithDefault(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	return toInt(v)
}

func requireCoord(args map[string]interface{}, key string) (int, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s coordinate is required", key)
	}
	return toInt(v), nil
}

func winMouseButtonFlags(button string) (down, up uint32) {
	switch button {
	case "right":
		return 0x0008, 0x0010 // MOUSEEVENTF_RIGHTDOWN, MOUSEEVENTF_RIGHTUP
	case "middle":
		return 0x0020, 0x0040 // MOUSEEVENTF_MIDDLEDOWN, MOUSEEVENTF_MIDDLEUP
	default:
		return 0x0002, 0x0004 // MOUSEEVENTF_LEFTDOWN, MOUSEEVENTF_LEFTUP
	}
}

func winEscapeForSendKeys(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

// winEscapeSendKeysSpecials wraps SendKeys special characters (+^%~()) in braces
// so they are typed literally rather than interpreted as modifiers.
func winEscapeSendKeysSpecials(s string) string {
	specials := map[byte]string{
		'+': "{+}", '^': "{^}", '%': "{%}", '~': "{~}",
		'(': "{(}", ')': "{)}", '{': "{{}", '}': "{}}",
		'[': "{[}", ']': "{]}",
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if escaped, ok := specials[s[i]]; ok {
			b.WriteString(escaped)
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	// Replace newlines, carriage returns, and tabs with spaces.
	// Strip other control characters, which cannot be typed via keystroke.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 32 && r != ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func appleScriptKeyCode(keys string) (string, bool) {
	m := map[string]string{
		"Return": "36", "Escape": "53", "Tab": "48", "Space": "49",
		"Delete": "51", "Left": "123", "Right": "124", "Down": "125", "Up": "126",
		"Home": "115", "End": "119", "PageUp": "116", "PageDown": "121",
		"F1": "122", "F2": "120", "F3": "99", "F4": "118", "F5": "96", "F6": "97",
		"F7": "98", "F8": "100", "F9": "101", "F10": "109", "F11": "103", "F12": "111",
	}
	code, ok := m[keys]
	return code, ok
}

func keysToAppleScriptModifiers(keys string) string {
	mods := map[string]string{
		"ctrl": "control down", "cmd": "command down",
		"alt": "option down", "shift": "shift down",
	}
	parts := strings.Split(keys, "+")
	var result []string
	for _, p := range parts {
		lower := strings.ToLower(strings.TrimSpace(p))
		if m, ok := mods[lower]; ok {
			result = append(result, m)
		}
	}
	return strings.Join(result, ", ")
}

// macQuartzClick performs a mouse click using Python Quartz (Core Graphics).
// This is the fallback when cliclick is not installed. Python with pyobjc is
// included on macOS by default; no additional brew packages required.
func macQuartzClick(ctx context.Context, x, y int, button string) error {
	clickType := "kCGEventLeftMouseDown"
	releaseType := "kCGEventLeftMouseUp"
	if button == "right" {
		clickType = "kCGEventRightMouseDown"
		releaseType = "kCGEventRightMouseUp"
	} else if button == "middle" {
		clickType = "kCGEventOtherMouseDown"
		releaseType = "kCGEventOtherMouseUp"
	}
	script := fmt.Sprintf(`
import Quartz
import time

def click(x, y, down, up):
    pos = Quartz.CGPoint(x, y)
    down_evt = Quartz.CGEventCreateMouseEvent(None, down, pos, 0)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, down_evt)
    time.sleep(0.05)
    up_evt = Quartz.CGEventCreateMouseEvent(None, up, pos, 0)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, up_evt)

click(%d, %d, Quartz.%s, Quartz.%s)
print("ok")
`, x, y, clickType, releaseType)
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("quartz click: %v — %s", err, out)
	}
	return nil
}

// linuxInputTool returns the best available input tool for the current session.
// Prefers ydotool (Wayland) over xdotool (X11).
func linuxInputTool() string {
	if _, err := exec.LookPath("ydotool"); err == nil {
		return "ydotool"
	}
	if _, err := exec.LookPath("xdotool"); err == nil {
		return "xdotool"
	}
	return ""
}

// linuxMoveArgs returns mousemove arguments adapted for the given tool.
// ydotool requires --absolute for coordinate-based moves; xdotool does not.
func linuxMoveArgs(tool string, x, y int) []string {
	switch tool {
	case "ydotool":
		return []string{"mousemove", "--absolute", fmt.Sprint(x), fmt.Sprint(y)}
	default:
		return []string{"mousemove", fmt.Sprint(x), fmt.Sprint(y)}
	}
}

// linuxClickArgs returns args for click with the given tool.
func linuxClickArgs(tool string, x, y int, btnCode string) []string {
	switch tool {
	case "ydotool":
		return []string{"mousemove", "--absolute", fmt.Sprint(x), fmt.Sprint(y), "click", btnCode}
	default:
		return []string{"mousemove", fmt.Sprint(x), fmt.Sprint(y), "click", btnCode}
	}
}

// linuxDblClickArgs returns args for double-click with the given tool.
func linuxDblClickArgs(tool string, x, y int) []string {
	switch tool {
	case "ydotool":
		return []string{"mousemove", "--absolute", fmt.Sprint(x), fmt.Sprint(y), "click", "1", "click", "1"}
	default:
		return []string{"mousemove", fmt.Sprint(x), fmt.Sprint(y), "click", "--repeat=2", "1"}
	}
}
