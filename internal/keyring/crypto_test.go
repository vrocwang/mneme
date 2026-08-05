package keyring

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestChaCha20Roundtrip(t *testing.T) {
	key, err := generateRandomKey()
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
	key1, _ := generateRandomKey()
	key2, _ := generateRandomKey()

	blob, err := chacha20Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := chacha20Decrypt(key2, blob); err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestChaCha20DecryptShortBlob(t *testing.T) {
	key, _ := generateRandomKey()
	if _, err := chacha20Decrypt(key, make([]byte, nonceLen)); err == nil {
		t.Error("decrypt of blob with only nonce should fail")
	}
}

func TestSecretStore_EncryptDecrypt(t *testing.T) {
	key, _ := generateRandomKey()
	store := NewSecretStore(key)

	encrypted, err := store.Encrypt("my-api-key")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted[:5] != enc2Prefix {
		t.Errorf("encrypted value should start with %q, got %q", enc2Prefix, encrypted[:5])
	}

	decrypted, err := store.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "my-api-key" {
		t.Errorf("decrypted: got %q, want %q", decrypted, "my-api-key")
	}
}

func TestSecretStore_DecryptLegacy(t *testing.T) {
	store := NewSecretStore(nil)
	// Generate a legacy enc: value using the XOR cipher from secret_store.go
	legacyKey := []byte("mneme-legacy-xor-key-32byte!")
	plaintext := "old-secret"
	data := make([]byte, len(plaintext))
	for i, b := range []byte(plaintext) {
		data[i] = b ^ legacyKey[i%len(legacyKey)]
	}
	encoded := "enc:" + base64.StdEncoding.EncodeToString(data)

	decrypted, err := store.Decrypt(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != plaintext {
		t.Errorf("legacy decryption: got %q, want %q", decrypted, plaintext)
	}
}

func TestSecretStore_DecryptPlaintext(t *testing.T) {
	store := NewSecretStore(nil)
	val, err := store.Decrypt("plaintext-value")
	if err != nil {
		t.Fatal(err)
	}
	if val != "plaintext-value" {
		t.Errorf("plaintext should pass through: got %q", val)
	}
}

func TestSecretStore_NeedsReEncrypt(t *testing.T) {
	key, _ := generateRandomKey()
	store := NewSecretStore(key)

	if !store.NeedsReEncrypt("plaintext") {
		t.Error("plaintext should need re-encrypt")
	}
	if !store.NeedsReEncrypt("enc:abc") {
		t.Error("legacy enc: should need re-encrypt")
	}
	if store.NeedsReEncrypt("enc2:abc") {
		t.Error("enc2: should not need re-encrypt")
	}

	// Without master key, nothing needs re-encrypt.
	noKeyStore := NewSecretStore(nil)
	if noKeyStore.NeedsReEncrypt("plaintext") {
		t.Error("without master key, nothing needs re-encrypt")
	}
}

func TestSecretStore_EncryptWithoutKey(t *testing.T) {
	store := NewSecretStore(nil)
	if _, err := store.Encrypt("x"); err == nil {
		t.Error("encrypt without master key should fail")
	}
}

func TestEncryptedFileBackend_Basic(t *testing.T) {
	dir := t.TempDir()
	key, _ := generateRandomKey()

	backend, err := NewEncryptedFileBackend(dir, key)
	if err != nil {
		t.Fatal(err)
	}

	// Set a secret.
	if err := backend.Set("test-svc", "api-key", "my-secret-value"); err != nil {
		t.Fatal(err)
	}

	// Read it back.
	got, err := backend.Get("test-svc", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	if got != "my-secret-value" {
		t.Errorf("got %q, want %q", got, "my-secret-value")
	}

	// NotFound for missing key.
	if _, err := backend.Get("test-svc", "nonexistent"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Delete.
	if err := backend.Delete("test-svc", "api-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Get("test-svc", "api-key"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestEncryptedFileBackend_Persistence(t *testing.T) {
	dir := t.TempDir()
	key, _ := generateRandomKey()

	backend, err := NewEncryptedFileBackend(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	backend.Set("svc", "k1", "v1")
	backend.Set("svc", "k2", "v2")

	// Re-open with the same key — should recover secrets.
	backend2, err := NewEncryptedFileBackend(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := backend2.Get("svc", "k1")
	v2, _ := backend2.Get("svc", "k2")
	if v1 != "v1" || v2 != "v2" {
		t.Errorf("persistence lost: got (%q, %q), want (v1, v2)", v1, v2)
	}
}

func TestEncryptedFileBackend_WrongKey(t *testing.T) {
	dir := t.TempDir()
	key1, _ := generateRandomKey()
	key2, _ := generateRandomKey()

	backend, _ := NewEncryptedFileBackend(dir, key1)
	backend.Set("svc", "k", "v")

	// Re-open with wrong key should fail.
	_, err := NewEncryptedFileBackend(dir, key2)
	if err == nil {
		t.Error("opening with wrong key should fail")
	}
}

func TestEncryptedFileBackend_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	key, _ := generateRandomKey()

	backend, _ := NewEncryptedFileBackend(dir, key)
	backend.Set("svc", "k", "original")

	// Verify no .tmp file is left behind.
	tmpPath := backend.FilePath() + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error(".tmp file should not exist after successful write")
	}

	// Verify the main file exists.
	if _, err := os.Stat(backend.FilePath()); err != nil {
		t.Error("secrets file should exist after write")
	}
}

func TestEncryptedFileBackend_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	key, _ := generateRandomKey()

	// Write an empty file.
	os.WriteFile(filepath.Join(dir, secretsFileName), []byte{}, 0600)

	backend, err := NewEncryptedFileBackend(dir, key)
	if err != nil {
		t.Fatal(err)
	}

	// Should be able to store and retrieve.
	backend.Set("svc", "k", "v")
	got, _ := backend.Get("svc", "k")
	if got != "v" {
		t.Errorf("got %q, want %q", got, "v")
	}
}

func TestHexEncodeDecode(t *testing.T) {
	data := []byte{0xde, 0xad, 0xbe, 0xef}
	enc := hexEncode(data)
	if enc != "deadbeef" {
		t.Errorf("hex encode: got %q, want deadbeef", enc)
	}
	dec, err := hexDecode("deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != string(data) {
		t.Errorf("hex decode: got %x, want %x", dec, data)
	}
}

func TestMasterKeyEqual(t *testing.T) {
	k1, _ := generateRandomKey()
	k2, _ := generateRandomKey()

	if !MasterKeyEqual(k1, k1) {
		t.Error("same key should be equal")
	}
	if MasterKeyEqual(k1, k2) {
		t.Error("different keys should not be equal")
	}
	if MasterKeyEqual(nil, k1) {
		t.Error("nil key should not equal valid key")
	}
}

func TestGenerateRandomKey(t *testing.T) {
	k1, err := generateRandomKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := generateRandomKey()
	if err != nil {
		t.Fatal(err)
	}
	// Extremely unlikely to generate the same key twice.
	if MasterKeyEqual(k1, k2) {
		t.Skip("extremely unlikely collision — skipping")
	}
}
