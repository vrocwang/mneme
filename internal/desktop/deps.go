package desktop

import (
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
)

var checkDepsOnce sync.Once

// checkPlatformDeps logs warnings for missing platform-specific desktop
// automation tools. Called once via NewScreenCapture, not from app.go.
func checkPlatformDeps() {
	switch runtime.GOOS {
	case "darwin":
		for _, d := range []struct{ bin, what string }{
			{"cliclick", "mouse click automation (brew install cliclick)"},
			{"rec", "microphone recording (brew install sox)"},
		} {
			if _, err := exec.LookPath(d.bin); err != nil {
				slog.Warn("desktop: optional tool not found", "tool", d.bin, "note", d.what)
			}
		}
	case "linux":
		_, errX := exec.LookPath("xdotool")
		_, errY := exec.LookPath("ydotool")
		if errX != nil && errY != nil {
			slog.Warn("desktop: no Linux mouse tool found — install xdotool (X11) or ydotool (Wayland)")
		}
	}
}
