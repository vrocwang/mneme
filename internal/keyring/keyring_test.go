package keyring

import "testing"

func TestFileStore_RoundTrip(t *testing.T) {
	s := NewFileStore(t.TempDir())

	if err := s.Set("test-svc", "api-key", "secret-value"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	val, err := s.Get("test-svc", "api-key")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if val != "secret-value" {
		t.Errorf("expected secret-value, got %s", val)
	}
}

func TestFileStore_Delete(t *testing.T) {
	s := NewFileStore(t.TempDir())

	s.Set("test", "key", "val")
	s.Delete("test", "key")

	_, err := s.Get("test", "key")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStore_MissingKey(t *testing.T) {
	s := NewFileStore(t.TempDir())
	_, err := s.Get("nonexistent", "key")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
