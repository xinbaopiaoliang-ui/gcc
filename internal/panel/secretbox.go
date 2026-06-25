package panel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const secretBoxPrefix = "v1"

type SecretBox struct {
	aead cipher.AEAD
}

func NewSecretBox(masterKey string) (*SecretBox, error) {
	masterKey = strings.TrimSpace(masterKey)
	if masterKey == "" {
		return nil, errors.New("master key is required")
	}
	key := sha256.Sum256([]byte(masterKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func (b *SecretBox) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if b == nil || b.aead == nil {
		return "", errors.New("secret box is not configured")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := b.aead.Seal(nil, nonce, []byte(plaintext), nil)
	return strings.Join([]string{
		secretBoxPrefix,
		base64.RawURLEncoding.EncodeToString(nonce),
		base64.RawURLEncoding.EncodeToString(ciphertext),
	}, ":"), nil
}

func (b *SecretBox) Decrypt(encoded string) (string, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", nil
	}
	if b == nil || b.aead == nil {
		return "", errors.New("secret box is not configured")
	}
	parts := strings.Split(encoded, ":")
	if len(parts) != 3 || parts[0] != secretBoxPrefix {
		return "", errors.New("unsupported encrypted secret format")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("decrypt secret failed")
	}
	return string(plaintext), nil
}
