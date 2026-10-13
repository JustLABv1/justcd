package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// RFC 6238 SHA-1, six digits, 30-second steps. Window allows one step of clock skew.
func TOTP(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return "", err
	}
	h := hmac.New(sha1.New, key)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(step))
	h.Write(b[:])
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 15
	n := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000), nil
}
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for step := now.Unix()/30 - 1; step <= now.Unix()/30+1; step++ {
		expected, err := TOTP(secret, step)
		if err == nil && step > lastStep && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}
func NormalizeRecovery(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}
func RecoveryCodes() ([]string, [][]byte, error) {
	codes := []string{}
	hashes := [][]byte{}
	for i := 0; i < 10; i++ {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
		codes = append(codes, code)
		hashes = append(hashes, HashToken(code))
	}
	return codes, hashes, nil
}
