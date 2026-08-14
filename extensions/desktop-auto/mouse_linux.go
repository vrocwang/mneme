//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/simon/mneme/pkg/extsdk"
)

// mouseTool controls the mouse on Linux via xdotool.
func mouseTool(_ context.Context, args map[string]interface{}) extsdk.Result {
	action, _ := args["action"].(string)
	if action == "" {
		return extsdk.Result{Error: "action is required"}
	}

	if !hasXdotool() {
		return extsdk.Result{Error: "xdotool not found. Install with: sudo apt install xdotool"}
	}

	x, _ := floatFromArgs(args, "x")
	y, _ := floatFromArgs(args, "y")

	switch action {
	case "move":
		if x == 0 && y == 0 {
			return extsdk.Result{Error: "x and y coordinates required for move"}
		}
		out, err := execCmd("xdotool", "mousemove", strconv.Itoa(int(x)), strconv.Itoa(int(y)))
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("mouse move: %v", err)}
		}
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Mouse moved to (%d, %d)\n%s", int(x), int(y), out)}

	case "click":
		out, err := execCmd("xdotool", fmt.Sprintf("mousemove_relative"), "--", "0", "0")
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("mouse click: %v", err)}
		}
		args := []string{"click", "1"}
		if x != 0 || y != 0 {
			out2, err := execCmd("xdotool", "mousemove", strconv.Itoa(int(x)), strconv.Itoa(int(y)))
			if err != nil {
				return extsdk.Result{Error: fmt.Sprintf("mouse move: %v", err)}
			}
			out += out2
		}
		out2, err := execCmd("xdotool", args...)
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("mouse click: %v", err)}
		}
		return extsdk.Result{Success: true, Output: out + out2}

	case "dblclick":
		args := []string{"click", "--repeat", "2", "1"}
		if x != 0 || y != 0 {
			execCmd("xdotool", "mousemove", strconv.Itoa(int(x)), strconv.Itoa(int(y)))
		}
		out, err := execCmd("xdotool", args...)
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("mouse dblclick: %v", err)}
		}
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Double-click at (%d, %d)\n%s", int(x), int(y), out)}

	case "rightclick":
		if x != 0 || y != 0 {
			execCmd("xdotool", "mousemove", strconv.Itoa(int(x)), strconv.Itoa(int(y)))
		}
		out, err := execCmd("xdotool", "click", "3")
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("mouse rightclick: %v", err)}
		}
		return extsdk.Result{Success: true, Output: out}

	case "drag":
		dx, _ := floatFromArgs(args, "dx")
		dy, _ := floatFromArgs(args, "dy")
		if dx == 0 && dy == 0 {
			return extsdk.Result{Error: "dx and dy required for drag"}
		}
		if x != 0 || y != 0 {
			execCmd("xdotool", "mousemove", strconv.Itoa(int(x)), strconv.Itoa(int(y)))
		}
		execCmd("xdotool", "mousedown", "1")
		execCmd("xdotool", "mousemove_relative", "--", strconv.Itoa(int(dx)), strconv.Itoa(int(dy)))
		out, err := execCmd("xdotool", "mouseup", "1")
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("mouse drag: %v", err)}
		}
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Dragged by (%d, %d)\n%s", int(dx), int(dy), out)}

	case "scroll_up":
		amount, _ := floatFromArgs(args, "amount")
		if amount == 0 {
			amount = 3
		}
		out, err := execCmd("xdotool", "click", "--repeat", strconv.Itoa(int(amount)), "4")
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("scroll up: %v", err)}
		}
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Scrolled up %d\n%s", int(amount), out)}

	case "scroll_down":
		amount, _ := floatFromArgs(args, "amount")
		if amount == 0 {
			amount = 3
		}
		out, err := execCmd("xdotool", "click", "--repeat", strconv.Itoa(int(amount)), "5")
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("scroll down: %v", err)}
		}
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Scrolled down %d\n%s", int(amount), out)}

	default:
		return extsdk.Result{Error: fmt.Sprintf("unknown mouse action: %s (valid: move, click, dblclick, rightclick, drag, scroll_up, scroll_down)", action)}
	}
}

func hasXdotool() bool {
	_, err := exec.LookPath("xdotool")
	return err == nil
}

func execCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func floatFromArgs(args map[string]interface{}, key string) (float64, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func intFromArgs(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

func strSliceFromArgs(args map[string]interface{}, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
