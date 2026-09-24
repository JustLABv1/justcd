package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemoryKiB   = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 1
	passwordKeyLength   = 32
)

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", fmt.Errorf("password must be between 12 and 1024 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemoryKiB, passwordParallelism, passwordKeyLength)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemoryKiB, passwordIterations, passwordParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	if len(password) > 1024 {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return false
	}
	var memory uint32
	var iterations uint32
	var parallelism uint8
	for _, field := range strings.Split(parts[2], ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return false
		}
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return false
		}
		switch key {
		case "m":
			if n > passwordMemoryKiB {
				return false
			}
			memory = uint32(n)
		case "t":
			if n > passwordIterations {
				return false
			}
			iterations = uint32(n)
		case "p":
			if n > passwordParallelism {
				return false
			}
			parallelism = uint8(n)
		default:
			return false
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < 8 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(expected) != passwordKeyLength {
		return false
	}
	if memory != passwordMemoryKiB || iterations != passwordIterations || parallelism != passwordParallelism {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
