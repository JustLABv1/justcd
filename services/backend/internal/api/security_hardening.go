package api

import (
	"github.com/justlab/justcd/services/backend/internal/security"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) securityRoutes() {
	s.Mux.Handle("GET /api/v1/auth/sessions", s.requireAuth(http.HandlerFunc(s.activeSessions)))
	s.Mux.Handle("DELETE /api/v1/auth/sessions/{sessionID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.revokeSession))))
	s.Mux.Handle("GET /api/v1/auth/mfa", s.requireAuth(http.HandlerFunc(s.mfaStatus)))
	s.Mux.Handle("POST /api/v1/auth/mfa/enroll", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.enrollMFA))))
	s.Mux.Handle("POST /api/v1/auth/mfa/confirm", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.confirmMFA))))
	s.Mux.Handle("DELETE /api/v1/auth/mfa", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.removeMFA))))
}
func (s *Server) securityLimit(w http.ResponseWriter, r *http.Request, category, key string, limit int, window time.Duration) bool {
	allowed, err := s.Store.AllowSecurityAttempt(r.Context(), category+":"+key, limit, window)
	if err != nil {
		writeStoreError(w, "could not check request limit")
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
		if s.Logger != nil {
			s.Logger.WarnContext(r.Context(), "security rate limit exceeded", "category", category)
		}
		writeError(w, 429, "too many attempts; try again later")
		return false
	}
	return true
}
func (s *Server) activeSessions(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookieName)
	items, err := s.Store.ActiveSessions(r.Context(), currentUser(r).ID, security.HashToken(cookie.Value))
	if err != nil {
		writeStoreError(w, "could not list sessions")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeSession(r.Context(), currentUser(r).ID, r.PathValue("sessionID")); err != nil {
		writeError(w, 404, "session not found")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) mfaStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.Store.MFAStatus(r.Context(), currentUser(r).ID)
	if err != nil {
		writeStoreError(w, "could not read MFA status")
		return
	}
	writeJSON(w, 200, status)
}

type factorInput struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (s *Server) factorInput(w http.ResponseWriter, r *http.Request) (factorInput, bool) {
	var input factorInput
	if !s.securityLimit(w, r, "mfa-management", currentUser(r).ID, 8, 15*time.Minute) {
		return input, false
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, "password and code are required")
		return input, false
	}
	user, hash, err := s.Store.UserByEmail(r.Context(), currentUser(r).Email)
	if err != nil || user.ID != currentUser(r).ID || hash == "" || !security.VerifyPassword(hash, input.Password) {
		_ = s.Store.Audit(r.Context(), currentUser(r).ID, "mfa.reauthentication_failed", "user", currentUser(r).ID, nil)
		writeError(w, 401, "authentication failed")
		return input, false
	}
	return input, true
}
func (s *Server) enrollMFA(w http.ResponseWriter, r *http.Request) {
	_, ok := s.factorInput(w, r)
	if !ok {
		return
	}
	secret, err := security.NewTOTPSecret()
	if err != nil {
		writeError(w, 500, "could not create factor")
		return
	}
	user := currentUser(r)
	cipher, err := security.Encrypt(s.EncryptionKey, []byte(secret), "mfa:"+user.ID)
	if err != nil {
		writeError(w, 500, "could not encrypt factor")
		return
	}
	if err = s.Store.BeginMFA(r.Context(), user.ID, cipher); err != nil {
		writeError(w, 409, "MFA already enabled or unavailable")
		return
	}
	uri := "otpauth://totp/" + url.PathEscape("JustCD:"+user.Email) + "?secret=" + url.QueryEscape(secret) + "&issuer=JustCD&algorithm=SHA1&digits=6&period=30"
	writeJSON(w, 200, map[string]string{"secret": secret, "uri": uri})
}
func (s *Server) confirmMFA(w http.ResponseWriter, r *http.Request) {
	input, ok := s.factorInput(w, r)
	if !ok {
		return
	}
	codes, hashes, err := security.RecoveryCodes()
	if err != nil {
		writeError(w, 500, "could not create recovery codes")
		return
	}
	valid, err := s.Store.VerifyMFA(r.Context(), currentUser(r).ID, input.Code, "enroll", s.EncryptionKey, hashes)
	if err != nil {
		writeStoreError(w, "could not enable MFA")
		return
	}
	if !valid {
		writeError(w, 401, "invalid or expired code")
		return
	}
	// Enrollment revokes all previous sessions; issue a fresh session after proving the factor.
	token, csrf, err := s.issueSession(r, currentUser(r))
	if err != nil {
		writeSessionError(w, err)
		return
	}
	s.setSessionCookies(w, token, csrf)
	writeJSON(w, 200, map[string]any{"recoveryCodes": codes})
}
func (s *Server) removeMFA(w http.ResponseWriter, r *http.Request) {
	input, ok := s.factorInput(w, r)
	if !ok {
		return
	}
	valid, err := s.Store.VerifyMFA(r.Context(), currentUser(r).ID, input.Code, "remove", s.EncryptionKey, nil)
	if err != nil {
		writeStoreError(w, "could not remove MFA")
		return
	}
	if !valid {
		writeError(w, 401, "invalid or expired code")
		return
	}
	s.clearCookie(w, sessionCookieName, "/")
	s.clearCookie(w, csrfCookieName, "/")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// Check browser origins on credential-bearing auth requests before any state change.
func (s *Server) safeAuthOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, base := range []string{s.Config.FrontendURL, s.Config.PublicURL} {
		u, err := url.Parse(base)
		if err == nil && origin == u.Scheme+"://"+u.Host {
			return true
		}
	}
	return false
}
func (s *Server) hardeningRequest(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if s.Config.Production {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
		if r.Method != "GET" && r.Method != "HEAD" && !s.safeAuthOrigin(r) {
			writeError(w, 403, "origin validation failed")
			return false
		}
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/signup" {
			if !s.securityLimit(w, r, "auth-ip", remoteHost(r), 120, 15*time.Minute) {
				return false
			}
		}
		if strings.Contains(r.URL.Path, "/oidc/") {
			if !s.securityLimit(w, r, "oidc", remoteHost(r), 60, time.Minute) {
				return false
			}
		}
	}
	return true
}
