package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/observability"
)

func TestOIDCDiscoveryDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, reason       string
		status             int
		trailing, mismatch bool
	}{
		{name: "valid", status: 200},
		{name: "trailing slash preserved", status: 200, trailing: true},
		{name: "issuer mismatch", status: 200, mismatch: true, reason: "issuer_mismatch"},
		{name: "not found", status: 404, reason: "http_status"},
		{name: "unavailable", status: 503, reason: "http_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var issuer string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/openid-configuration" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					w.Write([]byte("secret-canary"))
					return
				}
				advertised := issuer
				if tc.mismatch {
					advertised += "/other"
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"issuer": advertised, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys"})
			}))
			defer server.Close()
			issuer = server.URL
			if tc.trailing {
				issuer += "/"
			}
			provider, reason, status, err := discoverOIDC(context.Background(), issuer)
			if reason != tc.reason || status != tc.status {
				t.Fatalf("reason=%s status=%d err=%v", reason, status, err)
			}
			if tc.reason == "" {
				if err != nil || provider == nil {
					t.Fatalf("discovery failed: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected discovery error")
			}
			var logs bytes.Buffer
			s := &Server{Logger: slog.New(observability.NewLogHandler(slog.NewJSONHandler(&logs, nil)))}
			response := httptest.NewRecorder()
			s.writeOIDCDiscoveryError(response, httptest.NewRequest("GET", "/", nil), "provider", reason, status)
			if strings.Contains(response.Body.String()+logs.String(), "secret-canary") {
				t.Fatal("provider response leaked")
			}
			if !strings.Contains(logs.String(), reason) {
				t.Fatalf("missing diagnostic: %s", logs.String())
			}
		})
	}
}

func TestOIDCDiscoveryTLSFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	_, reason, _, err := discoverOIDC(context.Background(), server.URL)
	if err == nil || reason != "tls_certificate" {
		t.Fatalf("reason=%s err=%v", reason, err)
	}
}
