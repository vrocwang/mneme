package keyring

import (
	"crypto/rand"
	"fmt"

	"testing"
)

func TestChaCha20Roundtrip(t *testing.T) {
	key, err := randomTestKey()
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("hello world")
	blob, err := chacha20Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := chacha20Decrypt(key, blob)
	if err != nil {
		t.Fatal(err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestChaCha20DecryptWrongKey(t *testing.T) {
	key1, _ := randomTestKey()
	key2, _ := randomTestKey()

	blob, err := chacha20Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := chacha20Decrypt(key2, blob); err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestChaCha20DecryptShortBlob(t *testing.T) {
	key, _ := randomTestKey()
	if _, err := chacha20Decrypt(key, make([]byte, nonceLen)); err == nil {
		t.Error("decrypt of blob with only nonce should fail")
	}
}

func randomTestKey() (*[32]byte, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fmt.Errorf("read random: %w", err)
	}
	return &b, nil
}
