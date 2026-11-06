package api

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	available, err := s.Store.SignupAvailable(r.Context())
	if err != nil {
		writeStoreError(w, "could not check setup state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"signupAvailable": available})
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
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
		writeStoreError(w, "could not create administrator")
		return
	}
	_ = s.Store.Audit(r.Context(), user.ID, "user.created", "user", user.ID, map[string]any{"email": user.Email, "isAdmin": true, "initialSetup": true})
	s.createSession(w, r, user)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings, settingsErr := s.Store.InstanceSettings(r.Context())
	if settingsErr != nil {
		writeStoreError(w, "could not check sign-in settings")
		return
	}
	if !s.localLoginAllowed(settings) {
		writeError(w, http.StatusForbidden, "password sign-in is turned off on this instance; use single sign-on")
		return
	}
	// Account limits avoid locking every user behind the shared frontend proxy.
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.Email) > 320 || input.Email == "" {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if !s.securityLimit(w, r, "login-account", input.Email, 8, 15*time.Minute) {
		return
	}
	user, passwordHash, err := s.Store.UserByEmail(r.Context(), input.Email)
	if err != nil {
		passwordHash = s.dummyHash
	}
	valid := security.VerifyPassword(passwordHash, input.Password)
	if err != nil || !valid {
		writeError(w, http.StatusUnauthorized, "email or password is incorrect")
		return
	}
	status, mfaErr := s.Store.MFAStatus(r.Context(), user.ID)
	if mfaErr != nil {
		writeStoreError(w, "could not check authentication")
		return
	}
	if status.Enabled {
		if input.Code == "" {
			_ = s.Store.Audit(r.Context(), user.ID, "mfa.challenge_requested", "user", user.ID, nil)
			writeJSON(w, 200, map[string]bool{"mfaRequired": true})
			return
		}
		valid, verifyErr := s.Store.VerifyMFA(r.Context(), user.ID, input.Code, "challenge", s.EncryptionKey, nil)
		if verifyErr != nil {
			writeStoreError(w, "could not check authentication")
			return
		}
		if !valid {
			writeError(w, 401, "authentication failed")
			return
		}
	}
	_ = s.Store.Audit(r.Context(), user.ID, "session.created", "user", user.ID, nil)
	s.createSession(w, r, user)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request, user store.User) {
	sessionToken, csrfToken, err := s.issueSession(r, user)
	if err != nil {
		writeSessionError(w, err)
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
		return "", "", &sessionStoreError{err: err}
	}
	return sessionToken, csrfToken, nil
}

type sessionStoreError struct{ err error }

func (e *sessionStoreError) Error() string { return e.err.Error() }
func (e *sessionStoreError) Unwrap() error { return e.err }

func writeSessionError(w http.ResponseWriter, err error) {
	var storeError *sessionStoreError
	if errors.As(err, &storeError) {
		writeStoreError(w, "could not create session")
		return
	}
	writeError(w, http.StatusInternalServerError, "could not create session")
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	// The page uses this to hide "New workspace" when the instance restricts it; the create endpoint enforces it.
	canCreateWorkspace := user.IsAdmin
	if !canCreateWorkspace && user.EmailVerified {
		if settings, err := s.Store.InstanceSettings(r.Context()); err == nil {
			canCreateWorkspace = settings.WorkspaceCreation == "anyone"
		} else {
			canCreateWorkspace = true
		}
	}
	response := map[string]any{"user": user, "canCreateWorkspace": canCreateWorkspace}
	if user.IsAdmin {
		// Lets the shell show that someone is waiting; a failure only hides the indicator.
		if n, err := s.Store.CountPendingAccounts(r.Context()); err == nil {
			response["pendingAccountApprovals"] = n
		}
	}
	writeJSON(w, http.StatusOK, response)
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

// register creates a password account for someone with none, when an administrator has opened registration.
// The account is unverified: it can sign in but has no workspace access until approved.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Store.InstanceSettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not check registration settings")
		return
	}
	if !settings.LocalRegistrationEnabled || !s.localLoginAllowed(settings) {
		writeError(w, http.StatusForbidden, "registration is not open on this instance")
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
	// Tighter than the generic sign-in limits: registration writes rows and burns password hashing.
	if !s.securityLimit(w, r, "register-ip", remoteHost(r), 10, time.Hour) || !s.securityLimit(w, r, "register-email", input.Email, 3, time.Hour) {
		return
	}
	if err := store.CheckEmailDomain(input.Email, settings.OIDCAllowedDomains); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	hash, err := security.HashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := s.Store.CreateSelfRegisteredUser(r.Context(), store.User{ID: store.NewID(), Email: input.Email, DisplayName: input.DisplayName}, hash)
	if errors.Is(err, store.ErrEmailTaken) {
		writeError(w, http.StatusConflict, "an account with this email address already exists; sign in instead")
		return
	}
	if err != nil {
		writeStoreError(w, "could not create account")
		return
	}
	_ = s.Store.Audit(r.Context(), user.ID, "user.registered", "user", user.ID, map[string]any{"email": user.Email})
	s.createSession(w, r, user)
}

// approveUser lets an administrator vouch for a self-registered account.
func (s *Server) approveUser(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.ApproveUser(r.Context(), r.PathValue("userID"), currentUser(r).ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "there is no pending account with that ID")
			return
		}
		writeStoreError(w, "could not approve the account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"approved": true})
}
