package cryptoutil

import (
	"bytes"
	"testing"
)

func TestNewKey(t *testing.T) {
	secret := []byte("my-secret-key")
	salt := []byte("random-salt-1234")
	key := NewKey(secret, salt)

	if len(key) != 32 {
		t.Errorf("NewKey() length = %d, want 32", len(key))
	}

	// Verify it's consistent.
	if !bytes.Equal(NewKey(secret, salt), key) {
		t.Error("NewKey() is not consistent")
	}

	// Verify different secrets produce different keys.
	if bytes.Equal(NewKey([]byte("different"), salt), key) {
		t.Error("NewKey() produced same key for different secrets")
	}

	// Verify different salts produce different keys.
	if bytes.Equal(NewKey(secret, []byte("different-salt")), key) {
		t.Error("NewKey() produced same key for different salts")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key := NewKey([]byte("test-secret"), []byte("salt"))

	// Create a copy of plaintext because encrypt will clear it.
	originalText := []byte("hello world, this is a secret message")
	plainText := make([]byte, len(originalText))
	copy(plainText, originalText)

	cipherText, err := Encrypt(plainText, key)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}

	if len(cipherText) == 0 {
		t.Fatal("Encrypt() returned empty cipher text")
	}

	if bytes.Equal(cipherText, originalText) {
		t.Fatal("Encrypt() returned plain text as cipher text")
	}

	// Verify plaintext was cleared.
	expectedCleared := make([]byte, len(originalText))
	if !bytes.Equal(plainText, expectedCleared) {
		t.Errorf("Encrypt() did not clear plaintext, got %v", plainText)
	}

	decrypted, err := Decrypt(cipherText, key)
	if err != nil {
		t.Fatalf("Decrypt() failed: %v", err)
	}

	if !bytes.Equal(decrypted, originalText) {
		t.Errorf("Decrypt() = %q, want %q", string(decrypted), string(originalText))
	}
}

func TestDecryptErrors(t *testing.T) {
	key := NewKey([]byte("test-secret"), []byte("salt"))
	wrongKey := NewKey([]byte("wrong-secret"), []byte("salt"))

	plainText := []byte("hello world")
	cipherText, err := Encrypt(plainText, key)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}

	t.Run("WrongKey", func(t *testing.T) {
		_, err := Decrypt(cipherText, wrongKey)
		if err == nil {
			t.Error("Decrypt() with wrong key should fail")
		}
	})

	t.Run("TooShort", func(t *testing.T) {
		_, err := Decrypt([]byte("AAA"), key) // too short for gcm nonce
		if err == nil {
			t.Error("Decrypt() with too short cipher text should fail")
		}
	})
}

func TestEncryptInvalidKey(t *testing.T) {
	plainText := []byte("test")
	_, err := Encrypt(plainText, []byte("too-short"))
	if err == nil {
		t.Error("Encrypt() with invalid key length should fail")
	}
}
