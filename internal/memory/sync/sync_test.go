package sync

import (
	"context"
	"testing"
)





func TestManager_RegisterAndSync(t *testing.T) {
	m := NewManager(nil)
	m.Register(&stubConnector{name: "test"})

	if len(m.connectors) != 1 {
		t.Errorf("expected 1 registered connector, got %d", len(m.connectors))
	}
}



// ── RSSConnector tests ───────────────────────────────────────────────




// ── WebPageConnector tests ───────────────────────────────────────────





// ── helpers ──────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type stubConnector struct{ name string }

func (s *stubConnector) Name() string { return s.name }
func (s *stubConnector) Sync(ctx context.Context) ([]Item, error) {
	return []Item{{Source: s.name, Content: "hello world"}}, nil
}
