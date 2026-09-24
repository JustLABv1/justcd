package api

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	available, err := s.Store.SignupAvailable(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check setup state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"signupAvailable": available})
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !s.allowLogin(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}
	var input struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Email = strings.TrimSpace(strings.ToLower(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if len(input.Email) > 320 || !strings.Contains(input.Email, "@") || len(input.DisplayName) == 0 || len(input.DisplayName) > 100 {
		writeError(w, http.StatusBadRequest, "a valid email and display name are required")
		return
	}
	hash, err := security.HashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := store.User{ID: store.NewID(), Email: input.Email, DisplayName: input.DisplayName, IsAdmin: true}
	if err := s.Store.CreateInitialAdmin(r.Context(), &user, hash); err != nil {
		if errors.Is(err, store.ErrAlreadyInitialized) {
			writeError(w, http.StatusConflict, "setup is complete; sign in instead")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create administrator")
		return
	}
	s.clearLogin(ip)
	s.createSession(w, r, user)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !s.allowLogin(ip) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, passwordHash, err := s.Store.UserByEmail(r.Context(), strings.TrimSpace(input.Email))
	if err != nil {
		passwordHash = s.dummyHash
	}
	valid := security.VerifyPassword(passwordHash, input.Password)
	if err != nil || !valid {
		writeError(w, http.StatusUnauthorized, "email or password is incorrect")
		return
	}
	s.clearLogin(ip)
	s.createSession(w, r, user)
}

func (s *Server) allowLogin(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-15 * time.Minute)
	tries := s.loginTries[ip][:0]
	for _, attempt := range s.loginTries[ip] {
		if attempt.After(cutoff) {
			tries = append(tries, attempt)
		}
	}
	if len(tries) >= 8 {
		s.loginTries[ip] = tries
		return false
	}
	s.loginTries[ip] = append(tries, now)
	return true
}

func (s *Server) clearLogin(ip string) {
	s.loginMu.Lock()
	delete(s.loginTries, ip)
	s.loginMu.Unlock()
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request, user store.User) {
	sessionToken, csrfToken, err := s.issueSession(r, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	s.setSessionCookies(w, sessionToken, csrfToken)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) issueSession(r *http.Request, user store.User) (string, string, error) {
	sessionToken, sessionHash, err := security.RandomToken(32)
	if err != nil {
		return "", "", err
	}
	csrfToken, csrfHash, err := security.RandomToken(32)
	if err != nil {
		return "", "", err
	}
	if err := s.Store.CreateSession(r.Context(), sessionHash, csrfHash, user.ID, time.Now().Add(s.Config.SessionLifetime)); err != nil {
		return "", "", err
	}
	return sessionToken, csrfToken, nil
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": currentUser(r)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.Store.DeleteSession(r.Context(), security.HashToken(cookie.Value))
	}
	s.clearCookie(w, sessionCookieName, "/")
	s.clearCookie(w, csrfCookieName, "/")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setSessionCookies(w http.ResponseWriter, sessionToken, csrfToken string) {
	secure := s.Config.SessionCookieSecure
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: sessionToken, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, Expires: time.Now().Add(s.Config.SessionLifetime), MaxAge: int(s.Config.SessionLifetime.Seconds())})
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: csrfToken, Path: "/", HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, Expires: time.Now().Add(s.Config.SessionLifetime), MaxAge: int(s.Config.SessionLifetime.Seconds())})
}

func (s *Server) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, HttpOnly: name == sessionCookieName, Secure: s.Config.SessionCookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func sameString(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
