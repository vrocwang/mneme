package security

import (
	"crypto/rand"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("sensitive memory data for user")

	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if string(ciphertext) == "sensitive memory data for user" {
		t.Error("ciphertext should not equal plaintext")
	}

	decrypted, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip failed: got %s", decrypted)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	k1 := make([]byte, 32)
	k2 := make([]byte, 32)
	rand.Read(k1)
	rand.Read(k2)

	ciphertext, _ := Encrypt(k1, []byte("data"))
	_, err := Decrypt(k2, ciphertext)
	if err == nil {
		t.Error("expected error with wrong key")
	}
}

func TestEncrypt_UniqueNonce(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	c1, _ := Encrypt(key, []byte("same data"))
	c2, _ := Encrypt(key, []byte("same data"))

	if string(c1) == string(c2) {
		t.Error("same plaintext should produce different ciphertext (unique nonce)")
	}
}
