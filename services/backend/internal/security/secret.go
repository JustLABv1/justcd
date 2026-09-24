package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

func ParseEncryptionKey(value string) ([]byte, error) {
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(value) == 32 {
		return []byte(value), nil
	}
	return nil, errors.New("JUSTCD_ENCRYPTION_KEY must be 32 raw bytes or a 32-byte hex/base64 value")
}

func Encrypt(key, plaintext []byte, purpose string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nil, nonce, plaintext, []byte(purpose))
	return append(nonce, sealed...), nil
}

func Decrypt(key, ciphertext []byte, purpose string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, fmt.Errorf("encrypted value is truncated")
	}
	plain, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(purpose))
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	return plain, nil
}

func RandomToken(size int) (string, []byte, error) {
	if size < 16 || size > 128 {
		return "", nil, fmt.Errorf("token size out of range")
	}
	plain := make([]byte, size)
	if _, err := rand.Read(plain); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(plain)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func HashToken(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }
