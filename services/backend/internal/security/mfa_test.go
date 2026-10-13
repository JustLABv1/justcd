package security

import (
	"testing"
	"time"
)

func TestTOTPAndReplay(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := TOTP(secret, 59/30)
	if err != nil || code != "287082" {
		t.Fatalf("RFC 6238 vector: %s %v", code, err)
	}
	now := time.Unix(59, 0)
	step, ok := VerifyTOTP(secret, code, now, -1)
	if !ok || step != 1 {
		t.Fatal("valid code rejected")
	}
	if _, ok = VerifyTOTP(secret, code, now, step); ok {
		t.Fatal("replay accepted")
	}
	if _, ok = VerifyTOTP(secret, "000000", now, -1); ok {
		t.Fatal("invalid code accepted")
	}
	if _, ok = VerifyTOTP(secret, code, now.Add(2*time.Minute), -1); ok {
		t.Fatal("expired code accepted")
	}
	if _, ok = VerifyTOTP(secret, code, now.Add(30*time.Second), -1); !ok {
		t.Fatal("clock skew rejected")
	}
}
func TestRecoveryEntropyAndSecretBinding(t *testing.T) {
	codes, hashes, err := RecoveryCodes()
	if err != nil || len(codes) != 10 {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i, code := range codes {
		if len(code) < 26 || seen[code] || string(hashes[i]) == code {
			t.Fatal("unsafe recovery code")
		}
		seen[code] = true
	}
	secret, err := NewTOTPSecret()
	if err != nil || len(secret) != 32 {
		t.Fatal("secret generation")
	}
	old := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	cipher, err := Encrypt(old, []byte(secret), "mfa:user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Decrypt(newKey, cipher, "mfa:user"); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = Decrypt(old, cipher, "mfa:other"); err == nil {
		t.Fatal("wrong purpose accepted")
	}
}
