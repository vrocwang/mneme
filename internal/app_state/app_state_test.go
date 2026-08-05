package app_state

import (
	"context"
	"testing"
	"time"
)

func TestManagerInitialPhase(t *testing.T) {
	m := NewManager("1.0.0", nil)
	if m.Phase() != PhaseBooting {
		t.Fatalf("expected booting, got %s", m.Phase())
	}
}

func TestManagerTransition(t *testing.T) {
	m := NewManager("1.0.0", nil)
	m.Transition(PhaseReady)

	if !m.IsReady() {
		t.Fatal("expected IsReady after PhaseReady")
	}

	status := m.Current()
	if status.Phase != PhaseReady {
		t.Fatalf("expected ready, got %s", status.Phase)
	}
	if status.Version != "1.0.0" {
		t.Fatalf("expected 1.0.0, got %s", status.Version)
	}
}

func TestManagerPhaseChangeCallback(t *testing.T) {
	m := NewManager("1.0.0", nil)

	called := make(chan Phase, 1)
	m.OnPhaseChange(func(p Phase) {
		select {
		case called <- p:
		default:
		}
	})

	m.Transition(PhaseReady)

	select {
	case p := <-called:
		if p != PhaseReady {
			t.Fatalf("expected ready, got %s", p)
		}
	case <-time.After(time.Second):
		t.Fatal("callback not called")
	}
}

func TestManagerWaitUntilReady(t *testing.T) {
	m := NewManager("1.0.0", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(50 * time.Millisecond)
		m.Transition(PhaseConnecting)
		time.Sleep(50 * time.Millisecond)
		m.Transition(PhaseReady)
	}()

	if err := m.WaitUntilReady(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubsystemHealth(t *testing.T) {
	m := NewManager("1.0.0", nil)

	m.SetSubsystemHealth("database", "healthy", "connected")
	m.SetSubsystemHealth("cache", "degraded", "slow response")

	h, ok := m.SubsystemHealth("database")
	if !ok || h.Status != "healthy" {
		t.Fatalf("expected healthy database, got %v", h)
	}

	h2, ok := m.SubsystemHealth("cache")
	if !ok || h2.Status != "degraded" {
		t.Fatalf("expected degraded cache, got %v", h2)
	}
}

func TestIsReady(t *testing.T) {
	m := NewManager("1.0.0", nil)
	if m.IsReady() {
		t.Fatal("expected not ready during booting")
	}
	m.Transition(PhaseReady)
	if !m.IsReady() {
		t.Fatal("expected ready")
	}
	m.Transition(PhaseDegraded)
	if !m.IsReady() {
		t.Fatal("expected ready in degraded state")
	}
	m.Transition(PhaseError)
	if m.IsReady() {
		t.Fatal("expected not ready in error state")
	}
}
