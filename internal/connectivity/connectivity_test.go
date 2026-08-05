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

func TestFormatReport(t *testing.T) {
	s := &Status{
		Timestamp: "2026-06-01T00:00:00Z",
		Internet:  ComponentStatus{Status: "ok", Latency: "50ms"},
		DNS:       ComponentStatus{Status: "ok", Latency: "5ms", Message: "Resolved to 8.8.8.8"},
		Ports: []PortStatus{
			{Host: "localhost", Port: 11434, Status: "error", Message: "connection refused"},
		},
	}
	report := FormatReport(s)
	if report == "" {
		t.Error("expected non-empty report")
	}
}

func TestQuickCheck(t *testing.T) {
	// Quick check may fail in CI without internet — just verifies it doesn't panic.
	result := QuickCheck()
	t.Logf("quick internet check: %v", result)
}

func TestStatusIcon(t *testing.T) {
	if statusIcon("ok") != "[OK]" {
		t.Errorf("expected [OK], got %s", statusIcon("ok"))
	}
	if statusIcon("degraded") != "[WARN]" {
		t.Errorf("expected [WARN], got %s", statusIcon("degraded"))
	}
	if statusIcon("error") != "[FAIL]" {
		t.Errorf("expected [FAIL], got %s", statusIcon("error"))
	}
}
