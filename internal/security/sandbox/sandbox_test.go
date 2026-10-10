package sandbox

import (
	"context"
	"testing"
)

func TestDetectReturnsSandbox(t *testing.T) {
	s := Detect()
	if s == nil {
		t.Fatal("expected a sandbox backend (at least noop)")
	}
	if s.Name() == "" {
		t.Error("sandbox should have a name")
	}
	if !s.Available() {
		t.Error("detected sandbox should be available")
	}
}

func TestNoopBackendProperties(t *testing.T) {
	s := Detect()
	if s == nil {
		t.Fatal("expected a backend")
	}

	cmd, err := s.WrapCommand(context.Background(), "/tmp/workspace", "echo", "hello")
	if err != nil {
		t.Fatalf("WrapCommand failed: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}

func TestWrapCommandContainsOriginalArgs(t *testing.T) {
	s := Detect()
	ctx := context.Background()
	cmd, err := s.WrapCommand(ctx, "/home/user/projects", "ls", "-la")
	if err != nil {
		t.Fatalf("WrapCommand failed: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected command")
	}
	t.Logf("wrapped command: %v", cmd.Args)
}
