// Package overlay manages transparent desktop overlay windows for visual
// feedback and ambient information display. Platform-specific window creation
// is delegated to build-tagged files.
package overlay

import (
	"context"
	"fmt"
	"image/color"
	"log/slog"
	"sync"
)

// Position describes where an overlay window is placed on screen.
type Position struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Anchor string `json:"anchor"` // "top-left", "top-right", "bottom-left", "bottom-right", "center"
}

// Content describes what to render in the overlay.
type Content struct {
	Text      string     `json:"text,omitempty"`
	ImagePath string     `json:"image_path,omitempty"`
	HTML      string     `json:"html,omitempty"`
	Color     color.RGBA `json:"color,omitempty"`
	FontSize  int        `json:"font_size,omitempty"`
	Opacity   float64    `json:"opacity"` // 0.0 - 1.0
}

// State tracks whether the overlay is currently shown.
type State string

const (
	StateHidden  State = "hidden"
	StateShowing State = "showing"
	StateVisible State = "visible"
	StateError   State = "error"
)

// Window represents a single overlay window.
type Window struct {
	ID       string
	Position Position
	Content  Content
	State    State
}

// Manager manages overlay windows. Platform-specific implementations
// handle actual window creation via build tags.
type Manager struct {
	mu      sync.RWMutex
	windows map[string]*Window
	log     *slog.Logger
}

// NewManager creates a new overlay manager.
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		windows: make(map[string]*Window),
		log:     log.With("component", "overlay"),
	}
}

// Show creates or updates an overlay window with the given content.
func (m *Manager) Show(ctx context.Context, id string, pos Position, content Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	win := &Window{
		ID:       id,
		Position: pos,
		Content:  content,
		State:    StateShowing,
	}
	m.windows[id] = win

	if err := showPlatform(ctx, win); err != nil {
		win.State = StateError
		return fmt.Errorf("show overlay %q: %w", id, err)
	}

	win.State = StateVisible
	m.log.Debug("overlay shown", "id", id, "pos", fmt.Sprintf("%dx%d", pos.Width, pos.Height))
	return nil
}

// Hide removes an overlay window.
func (m *Manager) Hide(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := hidePlatform(id); err != nil {
		return fmt.Errorf("hide overlay %q: %w", id, err)
	}

	if win, ok := m.windows[id]; ok {
		win.State = StateHidden
	}
	delete(m.windows, id)
	m.log.Debug("overlay hidden", "id", id)
	return nil
}

// HideAll removes all overlay windows.
func (m *Manager) HideAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.windows {
		if err := hidePlatform(id); err != nil {
			m.log.Warn("failed to hide overlay", "id", id, "error", err)
		}
	}
	m.windows = make(map[string]*Window)
	m.log.Debug("all overlays hidden")
}

// List returns all active overlay windows.
func (m *Manager) List() []Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Window, 0, len(m.windows))
	for _, w := range m.windows {
		out = append(out, *w)
	}
	return out
}
