package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/config"
)

func TestMCPTransportBoundary(t *testing.T) {
	s := &Server{Config: config.Config{PublicURL: "https://cd.example.test"}}
	h := s.mcpHandler()
	for _, tc := range []struct {
		name, method, origin, site string
		want                       int
	}{
		{"unauthenticated", "POST", "", "", 401},
		{"cross origin", "POST", "https://evil.test", "", 403},
		{"null origin", "POST", "null", "", 403},
		{"cross site", "POST", "", "cross-site", 403},
		{"get", "GET", "", "", 405},
		{"delete", "DELETE", "", "", 405},
		{"same origin requires auth", "POST", "https://cd.example.test", "same-origin", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/v1/mcp", strings.NewReader(`{}`))
			r.Host = "cd.example.test"
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestMCPHostAndOriginAllowlist(t *testing.T) {
	s := &Server{Config: config.Config{PublicURL: "https://cd.example.test", FrontendURL: "https://ui.example.test"}}
	for _, host := range []string{"evil.test", "cd.example.test.evil.test", "localhost:8080", "127.0.0.1:8080", "cd.example.test:8443", "user@cd.example.test"} {
		if s.mcpHostAllowed(host) {
			t.Fatalf("accepted host %s", host)
		}
	}
	for _, host := range []string{"cd.example.test", "ui.example.test"} {
		if !s.mcpHostAllowed(host) {
			t.Fatalf("denied host %s", host)
		}
	}
	for _, origin := range []string{"null", "https://evil.test", "https://cd.example.test/path", "https://user@cd.example.test", "https://cd.example.test?x=1", "https://cd.example.test#x", "http://cd.example.test"} {
		if s.mcpOriginAllowed(origin) {
			t.Fatalf("accepted origin %s", origin)
		}
	}
	for _, origin := range []string{"https://cd.example.test", "https://ui.example.test"} {
		if !s.mcpOriginAllowed(origin) {
			t.Fatalf("denied origin %s", origin)
		}
	}
	h := s.mcpHandler()
	r := httptest.NewRequest("POST", "http://evil.test/api/v1/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("DNS rebinding host accepted: %d", w.Code)
	}
}
func TestMCPInputLimits(t *testing.T) {
	s := &Server{}
	for _, in := range []mcpInput{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Offset: 10001}, {WorkspaceID: strings.Repeat("x", 129)}, {ApplicationID: strings.Repeat("x", 129)}} {
		if _, err := s.readMCPPage(context.Background(), mcpApplications, false, in); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
}

var _ http.Handler = (*Server)(nil)
