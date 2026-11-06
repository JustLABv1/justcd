package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeDomains(t *testing.T) {
	got, err := NormalizeDomains([]string{" Example.COM ", "@corp.example.org", "example.com", "", "sub.team-a.io"})
	if err != nil || !reflect.DeepEqual(got, []string{"example.com", "corp.example.org", "sub.team-a.io"}) {
		t.Fatalf("domains = %v, err = %v", got, err)
	}
	for _, bad := range []string{"localhost", "no spaces.com", "-lead.com", "trail-.com", "a..b.com", "user@example.com", "https://example.com", "ex_ample.com"} {
		var rejected *AccessError
		if _, err := NormalizeDomains([]string{bad}); !errors.As(err, &rejected) {
			t.Errorf("%q was accepted", bad)
		}
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = "d" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + ".example.com"
	}
	if _, err := NormalizeDomains(many); err == nil {
		t.Fatal("more than the maximum number of domains was accepted")
	}
}

func TestCheckEmailDomain(t *testing.T) {
	if err := CheckEmailDomain("anyone@anywhere.test", nil); err != nil {
		t.Fatalf("an empty allow list allows everyone: %v", err)
	}
	allowed := []string{"corp.example.com"}
	if err := CheckEmailDomain("Dev@CORP.example.com", allowed); err != nil {
		t.Fatalf("matching is case-insensitive: %v", err)
	}
	for _, email := range []string{"dev@example.com", "dev@evil-corp.example.com", "dev@corp.example.com.evil.test", "dev@sub.corp.example.com", "no-at-sign"} {
		if err := CheckEmailDomain(email, allowed); !errors.Is(err, ErrOIDCDomainNotAllowed) {
			t.Errorf("%q: err = %v", email, err)
		}
	}
}

func TestLocalLoginCanOnlyBeDisabledWhenSSOWorks(t *testing.T) {
	cases := []struct {
		name      string
		readiness AccessReadiness
		blocked   bool
	}{
		{"no provider", AccessReadiness{}, true},
		{"provider but nobody has used it", AccessReadiness{EnabledProviders: 1}, true},
		{"an administrator signs in with SSO", AccessReadiness{EnabledProviders: 1, SSOAdmins: 1}, false},
	}
	for _, test := range cases {
		if got := test.readiness.CanDisableLocalLogin() != nil; got != test.blocked {
			t.Errorf("%s: blocked = %v", test.name, got)
		}
	}
}
