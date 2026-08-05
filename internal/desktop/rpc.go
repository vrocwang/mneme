package desktop

import (
	"context"
	"time"
)

// CompanionProvider is the interface for lazy-lookup of runtime objects.
// App satisfies this; DesktopRPC calls GetCompanionLoop / GetContext at
// activation time rather than at Bind registration time.
type CompanionProvider interface {
	GetCompanionLoop() *CompanionLoop
	GetContext() context.Context
}

// RPC provides Wails-bound desktop companion methods.
type DesktopRPC struct {
	app         CompanionProvider
	screenIntel *ScreenIntelLoop
}

// NewDesktopRPC creates a desktop RPC handler. Nothing is pre-captured;
// all runtime dependencies (companion loop, context) are looked up lazily
// via the app interface at call time, after OnStartup has initialised them.
func NewDesktopRPC(app CompanionProvider) *DesktopRPC {
	return &DesktopRPC{app: app}
}

// WithScreenIntel attaches a screen intelligence loop for RPC control.
func (r *DesktopRPC) WithScreenIntel(si *ScreenIntelLoop) *DesktopRPC {
	r.screenIntel = si
	return r
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
	if r.screenIntel == nil {
		return "Screen intelligence not available."
	}
	r.screenIntel.Start(r.app.GetContext())
	return "Screen intelligence started"
}

// StopScreenIntel stops the periodic screen intelligence loop.
func (r *DesktopRPC) StopScreenIntel() string {
	if r.screenIntel == nil {
		return "Screen intelligence not available."
	}
	r.screenIntel.Stop()
	return "Screen intelligence stopped"
}

// RegisterActivateCallback sets a callback invoked on push-to-talk activation.
func (r *DesktopRPC) RegisterActivateCallback() {
	if r.app.GetCompanionLoop() != nil {
		r.app.GetCompanionLoop().SetActivateCallback(func() {
			go r.app.GetCompanionLoop().Activate(r.app.GetContext())
		})
	}
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
	if r.screenIntel == nil {
		return "Screen intelligence not available."
	}
	if intervalSecs > 0 {
		r.screenIntel.SetInterval(time.Duration(intervalSecs) * time.Second)
	}
	r.screenIntel.Start(r.app.GetContext())
	return "Screen intelligence started"
}
