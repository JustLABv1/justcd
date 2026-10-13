package config

import (
	"strings"
	"testing"
)

func TestProductionDefaults(t *testing.T) {
	t.Setenv("JUSTCD_DATABASE_URL", "postgres://test.invalid/db")
	t.Setenv("JUSTCD_ENCRYPTION_KEY", strings.Repeat("k", 32))
	t.Setenv("JUSTCD_ENV", "production")
	t.Setenv("JUSTCD_PUBLIC_URL", "https://justcd.example")
	t.Setenv("JUSTCD_FRONTEND_URL", "https://justcd.example")
	t.Setenv("JUSTCD_COOKIE_SECURE", "")
	cfg, err := Load()
	if err != nil || !cfg.Production || !cfg.SessionCookieSecure {
		t.Fatalf("production defaults: %+v %v", cfg, err)
	}
	t.Setenv("JUSTCD_COOKIE_SECURE", "false")
	if _, err = Load(); err == nil {
		t.Fatal("insecure production cookie accepted")
	}
	t.Setenv("JUSTCD_COOKIE_SECURE", "true")
	t.Setenv("JUSTCD_PUBLIC_URL", "http://justcd.example")
	if _, err = Load(); err == nil {
		t.Fatal("insecure production URL accepted")
	}
	t.Setenv("JUSTCD_ENV", "development")
	if _, err = Load(); err != nil {
		t.Fatal("explicit development mode", err)
	}
}
