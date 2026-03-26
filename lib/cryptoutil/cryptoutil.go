// Package cryptoutil provides cryptographic utilities for encryption,
// decryption, and key derivation.
package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// NewKey derives a 32-byte key from a secret string using Argon2id.
func NewKey(secret, salt []byte) []byte {
	return argon2.IDKey(secret, salt, 1, 64*1024, 4, 32)
}

// Encrypt encrypts plain text using AES-GCM with the provided key. The key
// should be 32 bytes for AES-256. It clears the plainText slice before
// returning.
func Encrypt(plainText, key []byte) ([]byte, error) {
	defer clear(plainText)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	cipherText := gcm.Seal(nonce, nonce, plainText, nil)
	return cipherText, nil
}

// Decrypt decrypts cipher text using AES-GCM with the provided key.
func Decrypt(cipherText, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(cipherText) < nonceSize {
		return nil, errors.New("cipher text too short")
	}

	nonce, encryptedMessage := cipherText[:nonceSize], cipherText[nonceSize:]
	plainText, err := gcm.Open(nil, nonce, encryptedMessage, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plainText, nil
}
