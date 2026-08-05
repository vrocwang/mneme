package cron

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestScheduler_AddAndList(t *testing.T) {
	s := New(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	s.Add(&Job{ID: "j1", Name: "test", Schedule: "*/5m"})

	list := s.List()
	if len(list) != 1 {
		t.Errorf("expected 1 job, got %d", len(list))
	}
}

func TestScheduler_Remove(t *testing.T) {
	s := New(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	s.Add(&Job{ID: "j1", Name: "test", Schedule: "*/5m"})
	s.Remove("j1")
	if len(s.List()) != 0 {
		t.Error("expected empty job list after remove")
	}
}

func TestScheduler_NextRun(t *testing.T) {
	s := New(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	next := s.nextRun("*/30m")
	if next.IsZero() {
		t.Error("expected nonzero next run")
	}
}

func TestJob_HandlerFunc(t *testing.T) {
	called := false
	j := &Job{
		ID:      "test",
		Enabled: true,
		Handler: func(ctx context.Context) error {
			called = true
			return nil
		},
	}
	if err := j.Handler(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("expected handler to be called")
	}
}
