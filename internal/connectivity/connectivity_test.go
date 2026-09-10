package connectivity

import (
	"testing"
)

func TestRunDiagnostics(t *testing.T) {
	// Run without extra ports — internet check may fail in CI, that's fine.
	s := RunDiagnostics(nil)
	if s.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if s.Internet.Status == "" {
		t.Error("expected internet status to be set")
	}
	if s.DNS.Status == "" {
		t.Error("expected DNS status to be set")
	}
}

func TestRunDiagnostics_WithPorts(t *testing.T) {
	s := RunDiagnostics([]PortCheck{
		{Host: "127.0.0.1", Port: 11434, Desc: "Ollama"},
	})
	if len(s.Ports) != 1 {
		t.Errorf("expected 1 port result, got %d", len(s.Ports))
	}
	if s.Ports[0].Host != "127.0.0.1" {
		t.Errorf("unexpected host: %s", s.Ports[0].Host)
	}
}


func TestQuickCheck(t *testing.T) {
	// Quick check may fail in CI without internet — just verifies it doesn't panic.
	result := QuickCheck()
	t.Logf("quick internet check: %v", result)
}

