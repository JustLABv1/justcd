package api

import (
	"github.com/justlab/justcd/services/backend/internal/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersAndAuthOrigins(t *testing.T) {
	s := &Server{Mux: http.NewServeMux(), Config: config.Config{Production: true, PublicURL: "https://justcd.example", FrontendURL: "https://justcd.example"}}
	r := httptest.NewRequest("GET", "/missing", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	for _, header := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Content-Security-Policy", "Permissions-Policy", "Strict-Transport-Security"} {
		if w.Header().Get(header) == "" {
			t.Error("missing", header)
		}
	}
	for _, origin := range []string{"https://evil.example", "null", "https://justcd.example.evil.example", "http://justcd.example"} {
		r = httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		r.Header.Set("Origin", origin)
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-site login accepted", origin)
		}
		if w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("error headers missing")
		}
	}
	r = httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.Header.Set("Origin", "https://justcd.example")
	if !s.safeAuthOrigin(r) {
		t.Fatal("own origin rejected")
	}
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if s.safeAuthOrigin(r) {
		t.Fatal("fetch metadata ignored")
	}
}
