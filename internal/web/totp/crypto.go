package totp

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
)

func GenerateMasterKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}
	return key, nil
}

func EncryptSecret(plaintext []byte, masterKey []byte) (string, error) {
	if len(masterKey) != 32 {
		return "", fmt.Errorf("master key must be 32 bytes")
	}

	var key [32]byte
	copy(key[:], masterKey)

	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := secretbox.Seal(nil, plaintext, &nonce, &key)

	combined := append(nonce[:], ciphertext...)
	return base64.StdEncoding.EncodeToString(combined), nil
}

func DecryptSecret(ciphertextB64 string, masterKey []byte) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes")
	}

	combined, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	if len(combined) < 24 {
		return nil, fmt.Errorf("ciphertext too short")
	}

	var key [32]byte
	copy(key[:], masterKey)

	var nonce [24]byte
	copy(nonce[:], combined[:24])

	plaintext, ok := secretbox.Open(nil, combined[24:], &nonce, &key)
	if !ok {
		return nil, fmt.Errorf("failed to decrypt: invalid key or corrupted ciphertext")
	}

	return plaintext, nil
}
