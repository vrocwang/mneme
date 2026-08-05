package desktop

import "context"

// CompanionState represents the desktop companion's current mode.
type CompanionState string

const (
	CompanionIdle      CompanionState = "idle"
	CompanionListening CompanionState = "listening"
	CompanionThinking  CompanionState = "thinking"
	CompanionSpeaking  CompanionState = "speaking"
	CompanionShowing   CompanionState = "showing"
)

// Companion manages the desktop AI assistant lifecycle.
type Companion struct {
	State CompanionState
}

func NewCompanion() *Companion {
	return &Companion{State: CompanionIdle}
}

// Activate starts a voice/screen interaction.
func (c *Companion) Activate(ctx context.Context) error {
	c.State = CompanionListening
	return nil
}

// Deactivate returns to idle.
func (c *Companion) Deactivate() {
	c.State = CompanionIdle
}
