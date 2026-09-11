package desktop

import (
	"context"
	"time"
)

// CompanionProvider is the interface for lazy-lookup of runtime objects.
// App satisfies it; DesktopRPC resolves every dependency at call time rather
// than at Bind registration time, because Wails evaluates Bind before OnStartup
// creates these components.
type CompanionProvider interface {
	GetCompanionLoop() *CompanionLoop
	GetScreenIntelLoop() *ScreenIntelLoop
	GetContext() context.Context
}

// RPC provides Wails-bound desktop companion methods.
type DesktopRPC struct {
	app CompanionProvider
}

// NewDesktopRPC creates a desktop RPC handler. Nothing is pre-captured.
func NewDesktopRPC(app CompanionProvider) *DesktopRPC {
	return &DesktopRPC{app: app}
}

// ActivateCompanion starts the desktop companion loop.
func (r *DesktopRPC) ActivateCompanion() string {
	if r.app.GetCompanionLoop() == nil {
		return "Companion not available — no AI provider was configured at startup. Check Settings → Providers that at least one provider exists and is set as default (Settings → General → Default Provider). Then restart the application."
	}
	go r.app.GetCompanionLoop().Activate(r.app.GetContext())
	return "Companion activated"
}

// StartScreenIntel starts the periodic screen intelligence loop.
func (r *DesktopRPC) StartScreenIntel() string {
	si := r.app.GetScreenIntelLoop()
	if si == nil {
		return "Screen intelligence not available."
	}
	si.Start(r.app.GetContext())
	return "Screen intelligence started"
}

// StopScreenIntel stops the periodic screen intelligence loop.
func (r *DesktopRPC) StopScreenIntel() string {
	si := r.app.GetScreenIntelLoop()
	if si == nil {
		return "Screen intelligence not available."
	}
	si.Stop()
	return "Screen intelligence stopped"
}

// StartCompanionLoop starts the desktop companion loop.
func (r *DesktopRPC) StartCompanionLoop() string {
	if r.app.GetCompanionLoop() == nil {
		return "Companion not available."
	}
	if r.app.GetCompanionLoop().IsRunning() {
		return "Companion already active."
	}
	go r.app.GetCompanionLoop().Activate(r.app.GetContext())
	return "Companion loop started"
}

// StopCompanionLoop stops the desktop companion loop.
func (r *DesktopRPC) StopCompanionLoop() {
	if r.app.GetCompanionLoop() != nil {
		r.app.GetCompanionLoop().Stop()
	}
}

// GetVoiceEngines returns the names of active STT/TTS engines.
func (r *DesktopRPC) GetVoiceEngines() map[string]string {
	if r.app.GetCompanionLoop() == nil {
		return map[string]string{"stt": "none", "tts": "none"}
	}
	stt, tts := r.app.GetCompanionLoop().VoiceEngines()
	return map[string]string{"stt": stt, "tts": tts}
}

// StartScreenIntelligence starts screen intelligence with a configurable interval.
func (r *DesktopRPC) StartScreenIntelligence(intervalSecs int) string {
	si := r.app.GetScreenIntelLoop()
	if si == nil {
		return "Screen intelligence not available."
	}
	if intervalSecs > 0 {
		si.SetInterval(time.Duration(intervalSecs) * time.Second)
	}
	si.Start(r.app.GetContext())
	return "Screen intelligence started"
}
