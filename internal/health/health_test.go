package health

import "testing"

func TestCheck_HealthyByDefault(t *testing.T) {
	status := Check()
	if status.Status != "ok" {
		t.Errorf("expected ok, got %s", status.Status)
	}
}

func TestCheck_WithChecks(t *testing.T) {
	Register("db", func() CheckResult {
		return CheckResult{Name: "db", Status: "ok"}
	})
	Register("disk", func() CheckResult {
		return CheckResult{Name: "disk", Status: "error", Message: "disk full"}
	})

	status := Check()
	if status.Status != "degraded" {
		t.Errorf("expected degraded, got %s", status.Status)
	}
	if len(status.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(status.Checks))
	}
}
