package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"golang.org/x/oauth2"
)

type oidcClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	provider, err := s.Store.OIDCProviderByID(r.Context(), r.PathValue("providerID"))
	if err != nil || !provider.Enabled {
		writeError(w, http.StatusNotFound, "OIDC provider not found")
		return
	}
	clientSecret, err := security.Decrypt(s.EncryptionKey, provider.ClientSecret, "oidc-provider:"+provider.ID)
	if err != nil {
		s.Logger.Error("could not decrypt OIDC provider secret", "providerId", provider.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "OIDC provider is unavailable")
		return
	}
	providerClient, reason, status, err := discoverOIDC(ctx, provider.Issuer)
	if err != nil {
		s.writeOIDCDiscoveryError(w, r, provider.ID, reason, status)
		return
	}
	state, _, err := security.RandomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start OIDC login")
		return
	}
	nonce, _, err := security.RandomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start OIDC login")
		return
	}
	verifier := oauth2.GenerateVerifier()
	if err := s.Store.CreateOIDCState(r.Context(), security.HashToken(state), provider.ID, nonce, verifier, time.Now().Add(10*time.Minute)); err != nil {
		writeStoreError(w, "could not start OIDC login")
		return
	}
	callbackPath := "/api/v1/auth/oidc/" + provider.ID + "/callback"
	config := oauth2.Config{ClientID: provider.ClientID, ClientSecret: string(clientSecret), Endpoint: providerClient.Endpoint(), RedirectURL: provider.RedirectURL, Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}
	authURL := config.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.S256ChallengeOption(verifier))
	http.SetCookie(w, &http.Cookie{Name: "justcd_oidc_" + provider.ID, Value: state, Path: callbackPath, HttpOnly: true, Secure: s.Config.SessionCookieSecure, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(10 * time.Minute), MaxAge: 600})
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	providerID := r.PathValue("providerID")
	stateCookie, err := r.Cookie("justcd_oidc_" + providerID)
	if err != nil || !sameString(stateCookie.Value, r.URL.Query().Get("state")) {
		writeError(w, http.StatusBadRequest, "OIDC state validation failed")
		return
	}
	defer http.SetCookie(w, &http.Cookie{Name: stateCookie.Name, Value: "", Path: "/api/v1/auth/oidc/" + providerID + "/callback", HttpOnly: true, Secure: s.Config.SessionCookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	state, err := s.Store.ConsumeOIDCState(r.Context(), security.HashToken(stateCookie.Value))
	if err != nil || state.ProviderID != providerID {
		writeError(w, http.StatusBadRequest, "OIDC login state expired")
		return
	}
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		http.Redirect(w, r, s.Config.FrontendURL+"/login?error=provider", http.StatusFound)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "OIDC authorization code is missing")
		return
	}
	provider, err := s.Store.OIDCProviderByID(r.Context(), providerID)
	if err != nil || !provider.Enabled {
		writeError(w, http.StatusNotFound, "OIDC provider not found")
		return
	}
	clientSecret, err := security.Decrypt(s.EncryptionKey, provider.ClientSecret, "oidc-provider:"+provider.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "OIDC provider is unavailable")
		return
	}
	oidcProvider, reason, status, err := discoverOIDC(ctx, provider.Issuer)
	if err != nil {
		s.writeOIDCDiscoveryError(w, r, provider.ID, reason, status)
		return
	}
	oauthConfig := oauth2.Config{ClientID: provider.ClientID, ClientSecret: string(clientSecret), Endpoint: oidcProvider.Endpoint(), RedirectURL: provider.RedirectURL, Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}
	token, err := oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(state.Verifier))
	if err != nil {
		s.Logger.Warn("OIDC token exchange failed", "providerId", providerID, "error", err)
		writeError(w, http.StatusUnauthorized, "OIDC login could not be completed")
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		writeError(w, http.StatusUnauthorized, "OIDC provider returned no ID token")
		return
	}
	idToken, err := oidcProvider.Verifier(&oidc.Config{ClientID: provider.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "OIDC ID token validation failed")
		return
	}
	if !sameString(idToken.Nonce, state.Nonce) {
		writeError(w, http.StatusUnauthorized, "OIDC nonce validation failed")
		return
	}
	claims := oidcClaims{}
	if err := idToken.Claims(&claims); err != nil || claims.Subject == "" || claims.Email == "" || !claims.EmailVerified {
		writeError(w, http.StatusUnauthorized, "OIDC account must provide a verified email address")
		return
	}
	var raw map[string]json.RawMessage
	groups := []string{}
	if err := idToken.Claims(&raw); err == nil {
		groups, err = validOIDCClaim(raw[provider.GroupsClaim])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "OIDC group claim has an invalid format")
			return
		}
	}
	user, err := s.Store.ResolveOIDCUser(ctx, provider.ID, claims.Subject, strings.ToLower(claims.Email), claims.Name, groups)
	if err != nil {
		s.Logger.Error("OIDC user mapping failed", "providerId", providerID, "error", err)
		writeStoreError(w, "could not provision OIDC identity")
		return
	}
	sessionToken, csrfToken, err := s.issueSession(r, user)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	s.setSessionCookies(w, sessionToken, csrfToken)
	http.Redirect(w, r, s.Config.FrontendURL, http.StatusFound)
}

func (s *Server) listPublicProviders(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListOIDCProviders(r.Context())
	if err != nil {
		writeStoreError(w, "could not load login providers")
		return
	}
	public := make([]map[string]string, 0)
	for _, p := range items {
		if p.Enabled {
			public = append(public, map[string]string{"id": p.ID, "name": p.Name})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": public})
}

func (s *Server) listOIDCProviders(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListOIDCProviders(r.Context())
	if err != nil {
		writeStoreError(w, "could not load OIDC providers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createOIDCProvider(w http.ResponseWriter, r *http.Request) {
	s.saveOIDCProvider(w, r, false)
}
func (s *Server) updateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	s.saveOIDCProvider(w, r, true)
}

func (s *Server) saveOIDCProvider(w http.ResponseWriter, r *http.Request, updating bool) {
	var input struct {
		Name         string `json:"name"`
		Issuer       string `json:"issuer"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		RedirectURL  string `json:"redirectUrl"`
		GroupsClaim  string `json:"groupsClaim"`
		Enabled      *bool  `json:"enabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Issuer = strings.TrimSpace(input.Issuer)
	issuer, err := url.Parse(input.Issuer)
	if err != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || (issuer.Scheme != "https" && issuer.Hostname() != "localhost" && issuer.Hostname() != "127.0.0.1") {
		writeError(w, http.StatusBadRequest, "issuer must be an HTTPS URL (localhost may use HTTP)")
		return
	}
	if input.Name == "" || len(input.Name) > 100 || input.ClientID == "" || len(input.ClientID) > 256 || (!updating && input.ClientSecret == "") || len(input.ClientSecret) > 4096 {
		writeError(w, http.StatusBadRequest, "name, clientId, and clientSecret are required")
		return
	}
	id := store.NewID()
	var existing store.OIDCProvider
	if updating {
		id = r.PathValue("providerID")
		var err error
		existing, err = s.Store.OIDCProviderByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "OIDC provider not found")
			} else {
				writeStoreError(w, "could not load OIDC provider")
			}
			return
		}
	}
	redirect := s.Config.PublicURL + "/api/v1/auth/oidc/" + id + "/callback"
	if strings.TrimSpace(input.RedirectURL) != "" && strings.TrimSpace(input.RedirectURL) != redirect {
		writeError(w, http.StatusBadRequest, "redirectUrl must match the configured JustCD public URL callback")
		return
	}
	redirectURL, err := url.Parse(redirect)
	if err != nil || redirectURL.Host == "" || redirectURL.User != nil || redirectURL.RawQuery != "" || redirectURL.Fragment != "" || (redirectURL.Scheme != "https" && redirectURL.Hostname() != "localhost" && redirectURL.Hostname() != "127.0.0.1") {
		writeError(w, http.StatusBadRequest, "redirectUrl must be an absolute HTTPS URL")
		return
	}
	claim := strings.TrimSpace(input.GroupsClaim)
	if claim == "" {
		claim = "groups"
	}
	if len(claim) > 128 || strings.ContainsAny(claim, " \t\r\n") {
		writeError(w, http.StatusBadRequest, "groupsClaim must be a short claim name")
		return
	}
	enabled := true
	if updating {
		enabled = existing.Enabled
	}
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	cipher := existing.ClientSecret
	if input.ClientSecret != "" {
		cipher, err = security.Encrypt(s.EncryptionKey, []byte(input.ClientSecret), "oidc-provider:"+id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not encrypt OIDC client secret")
			return
		}
	}
	provider := store.OIDCProvider{ID: id, Name: input.Name, Issuer: input.Issuer, ClientID: input.ClientID, ClientSecret: cipher, RedirectURL: redirect, GroupsClaim: claim, Enabled: enabled}
	if updating {
		provider.CreatedAt = existing.CreatedAt
		if err := s.Store.UpdateOIDCProvider(r.Context(), provider); err != nil {
			if errors.Is(err, store.ErrOIDCIssuerInUse) {
				writeError(w, http.StatusConflict, "issuer cannot be changed after users have signed in; create a new provider instead")
				return
			}
			writeStoreError(w, "could not update OIDC provider")
			return
		}
		_ = s.Store.Audit(r.Context(), currentUser(r).ID, "oidc_provider.updated", "oidc_provider", id, map[string]string{"name": provider.Name, "issuer": provider.Issuer})
		provider.ClientSecret = nil
		writeJSON(w, http.StatusOK, provider)
		return
	}
	if err := s.Store.CreateOIDCProvider(r.Context(), provider); err != nil {
		writeStoreError(w, "could not create OIDC provider")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "oidc_provider.created", "oidc_provider", id, map[string]string{"name": provider.Name, "issuer": provider.Issuer})
	provider.ClientSecret = nil
	writeJSON(w, http.StatusCreated, provider)
}

func (s *Server) deleteOIDCProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("providerID")
	if err := s.Store.DeleteOIDCProvider(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "OIDC provider not found")
		} else {
			writeStoreError(w, "could not delete OIDC provider")
		}
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "oidc_provider.deleted", "oidc_provider", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addOIDCGroupRole(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Group       string `json:"group"`
		WorkspaceID string `json:"workspaceId"`
		Role        string `json:"role"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Group = strings.TrimSpace(input.Group)
	if input.Group == "" || len(input.Group) > 256 || input.WorkspaceID == "" || !validRole(input.Role) {
		writeError(w, http.StatusBadRequest, "group, workspaceId, and a valid role are required")
		return
	}
	if err := s.Store.AddOIDCGroupRole(r.Context(), r.PathValue("providerID"), input.Group, input.WorkspaceID, input.Role); err != nil {
		writeError(w, http.StatusBadRequest, "could not add group mapping")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "oidc_group_role.created", "oidc_provider", r.PathValue("providerID"), map[string]string{"group": input.Group, "workspaceId": input.WorkspaceID, "role": input.Role})
	writeJSON(w, http.StatusCreated, map[string]string{"group": input.Group, "workspaceId": input.WorkspaceID, "role": input.Role})
}

func validOIDCClaim(raw json.RawMessage) ([]string, error) {
	var values []string
	if len(raw) == 0 {
		return values, nil
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New("OIDC group claim must be an array of strings")
	}
	if len(values) > 200 {
		return nil, errors.New("OIDC group claim contains too many entries")
	}
	for _, value := range values {
		if value == "" || len(value) > 256 {
			return nil, errors.New("OIDC group claim contains an invalid entry")
		}
	}
	return values, nil
}
