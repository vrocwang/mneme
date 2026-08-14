package keyring

import (
	"os"
	"strings"
	"testing"
)

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

// TestFileStore_ReadLegacyPlaintext verifies that a value written by the old
// plaintext FileStore — including one longer than the ChaCha20-Poly1305 nonce
// (24 bytes) — is read correctly and transparently migrated to encrypted form.
func TestFileStore_ReadLegacyPlaintext(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)

	// A long legacy secret (> nonceLen) would previously be misclassified as
	// ciphertext and fail to decrypt.
	legacy := "sk-this-is-a-much-longer-legacy-api-key-than-24-bytes"
	p := s.path("svc", "key")
	if err := os.WriteFile(p, []byte(legacy), 0600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	got, err := s.Get("svc", "key")
	if err != nil {
		t.Fatalf("get legacy: %v", err)
	}
	if got != legacy {
		t.Errorf("expected legacy value %q, got %q", legacy, got)
	}

	// The file should now be encrypted (magic-prefixed), not plaintext.
	blob, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read migrated: %v", err)
	}
	if !strings.HasPrefix(string(blob), fileStoreMagic) {
		t.Errorf("expected migrated value to be encrypted with magic prefix, got prefix %q", string(blob[:min(len(blob), 12)]))
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

