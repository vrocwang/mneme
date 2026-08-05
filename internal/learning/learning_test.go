package learning

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestEngine_Reflect(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	e := New(log)

	learned := e.Reflect(context.Background(), "t1",
		"I prefer using local models for privacy",
		"I'll use local models from now on",
	)

	if len(learned) == 0 {
		t.Error("expected to learn from explicit preference")
	}
}

func TestEngine_Preferences(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	e := New(log)

	e.Reflect(context.Background(), "t1", "I like dark mode", "ok")
	prefs := e.Preferences()
	if len(prefs) == 0 {
		t.Error("expected stored preferences")
	}
}
