package cwd_jail

import "runtime"

// Detect returns the best available jail backend for the current platform.
// Priority: Landlock (Linux, kernel-native) > Seatbelt (macOS) >
// AppContainer (Windows) > noop (fallback).
func Detect() Backend {
	switch runtime.GOOS {
	case "linux":
		if lb := NewLandlock(); lb != nil && lb.IsAvailable() {
			return lb
		}
	case "darwin":
		if sb := NewSeatbelt(); sb != nil && sb.IsAvailable() {
			return sb
		}
	case "windows":
		if ac := NewAppContainer(); ac != nil && ac.IsAvailable() {
			return ac
		}
	}
	return &noopBackend{}
}
