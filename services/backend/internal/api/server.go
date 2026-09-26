package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/justlab/justcd/services/backend/internal/syncer"
)

type contextKey string

const userContextKey contextKey = "justcd-user"
const sessionCookieName = "justcd_session"
const csrfCookieName = "justcd_csrf"

type Server struct {
	Store         *store.Store
	Config        config.Config
	EncryptionKey []byte
	Logger        *slog.Logger
	Syncer        *syncer.Service
	dummyHash     string
	loginMu       sync.Mutex
	loginTries    map[string][]time.Time
	Mux           *http.ServeMux
}

func New(s *store.Store, cfg config.Config, logger *slog.Logger) (*Server, error) {
	dummy, err := security.HashPassword("justcd-invalid-login-dummy-password")
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{Store: s, Config: cfg, EncryptionKey: cfg.EncryptionKey, Logger: logger, Syncer: &syncer.Service{Store: s, EncryptionKey: cfg.EncryptionKey}, dummyHash: dummy, loginTries: map[string][]time.Time{}, Mux: http.NewServeMux()}
	server.routes()
	return server, nil
}

func (s *Server) routes() {
	s.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.Mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": "v1"})
	})
	s.Mux.HandleFunc("GET /api/v1/auth/providers", s.listPublicProviders)
	s.Mux.HandleFunc("GET /api/v1/auth/setup", s.setupStatus)
	s.Mux.HandleFunc("POST /api/v1/auth/signup", s.signup)
	s.Mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.Mux.HandleFunc("GET /api/v1/auth/oidc/{providerID}/start", s.oidcStart)
	s.Mux.HandleFunc("GET /api/v1/auth/oidc/{providerID}/callback", s.oidcCallback)

	s.Mux.Handle("GET /api/v1/auth/session", s.requireAuth(http.HandlerFunc(s.session)))
	s.Mux.Handle("POST /api/v1/auth/logout", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.logout))))
	s.Mux.Handle("GET /api/v1/projects", s.requireAuth(http.HandlerFunc(s.listProjects)))
	s.Mux.Handle("POST /api/v1/projects", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createProject))))
	s.Mux.Handle("PUT /api/v1/projects/{projectID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateProject))))
	s.Mux.Handle("PUT /api/v1/projects/{projectID}/approval-policy", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateProjectApprovalPolicy))))
	s.Mux.Handle("DELETE /api/v1/projects/{projectID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.deleteProject))))
	s.Mux.Handle("GET /api/v1/projects/{projectID}/members", s.requireAuth(http.HandlerFunc(s.listProjectMembers)))
	s.Mux.Handle("POST /api/v1/projects/{projectID}/members", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.setProjectMember))))
	s.Mux.Handle("PUT /api/v1/projects/{projectID}/members/{userID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateProjectMember))))
	s.Mux.Handle("DELETE /api/v1/projects/{projectID}/members/{userID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.removeProjectMember))))
	s.Mux.Handle("GET /api/v1/credentials", s.requireAuth(http.HandlerFunc(s.listCredentials)))
	s.Mux.Handle("POST /api/v1/credentials", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createCredential))))
	s.Mux.Handle("PUT /api/v1/credentials/{credentialID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateCredential))))
	s.Mux.Handle("GET /api/v1/clusters", s.requireAuth(http.HandlerFunc(s.listClusters)))
	s.Mux.Handle("POST /api/v1/clusters", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.createCluster)))))
	s.Mux.Handle("PUT /api/v1/clusters/{clusterID}", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.updateCluster)))))
	s.Mux.Handle("POST /api/v1/clusters/{clusterID}/test", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.testCluster))))
	s.Mux.Handle("GET /api/v1/clusters/{clusterID}/tests", s.requireAuth(http.HandlerFunc(s.listClusterPermissionTests)))
	s.Mux.Handle("POST /api/v1/clusters/{clusterID}/bindings", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createNamespaceBinding))))
	s.Mux.Handle("PUT /api/v1/clusters/{clusterID}/bindings/{namespace}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateNamespaceBinding))))
	s.Mux.Handle("GET /api/v1/clusters/{clusterID}/bindings", s.requireAuth(http.HandlerFunc(s.listNamespaceBindings)))
	s.Mux.Handle("GET /api/v1/clusters/{clusterID}/project-credential", s.requireAuth(http.HandlerFunc(s.getProjectClusterCredential)))
	s.Mux.Handle("PUT /api/v1/clusters/{clusterID}/project-credential", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.setProjectClusterCredential))))
	s.Mux.Handle("GET /api/v1/git-sources", s.requireAuth(http.HandlerFunc(s.listGitSources)))
	s.Mux.Handle("POST /api/v1/git-sources", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createGitSource))))
	s.Mux.Handle("PUT /api/v1/git-sources/{sourceID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateGitSource))))
	s.Mux.Handle("POST /api/v1/git-sources/{sourceID}/test", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.testGitSource))))
	s.Mux.Handle("GET /api/v1/applications", s.requireAuth(http.HandlerFunc(s.listApplications)))
	s.Mux.Handle("POST /api/v1/application-groups", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createApplicationGroup))))
	s.Mux.Handle("GET /api/v1/application-groups/{groupID}", s.requireAuth(http.HandlerFunc(s.getApplicationGroup)))
	s.Mux.Handle("PUT /api/v1/application-groups/{groupID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateApplicationGroup))))
	s.Mux.Handle("POST /api/v1/applications", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createApplication))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}", s.requireAuth(http.HandlerFunc(s.getApplication)))
	s.Mux.Handle("PUT /api/v1/applications/{applicationID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateApplication))))
	s.Mux.Handle("PUT /api/v1/applications/{applicationID}/approval-policy", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateApplicationApprovalPolicy))))
	s.Mux.Handle("DELETE /api/v1/applications/{applicationID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.deleteApplication))))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/cancel-decommission", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.cancelApplicationDecommission))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/kustomization", s.requireAuth(http.HandlerFunc(s.getApplicationKustomization)))
	s.Mux.Handle("PUT /api/v1/applications/{applicationID}/render-settings", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateApplicationRenderSettings))))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/plans", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createPlan))))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/retry", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.retryApplication))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/rollback-targets", s.requireAuth(http.HandlerFunc(s.listRollbackTargets)))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/rollback-plans", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createRollbackPlan))))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/rollback-state", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.updateRollbackState))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/ownership-conflict", s.requireAuth(http.HandlerFunc(s.getOwnershipConflict)))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/adopt", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.adoptApplicationResource))))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/adopt-batch", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.adoptApplicationResources))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/plans", s.requireAuth(http.HandlerFunc(s.listPlans)))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/ignore-rules", s.requireAuth(http.HandlerFunc(s.listIgnoreRules)))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/ignore-rules", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createIgnoreRule))))
	s.Mux.Handle("DELETE /api/v1/applications/{applicationID}/ignore-rules/{ruleID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.deleteIgnoreRule))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/ignore-selectors", s.requireAuth(http.HandlerFunc(s.listIgnoreSelectors)))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/ignore-selectors", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createIgnoreSelector))))
	s.Mux.Handle("DELETE /api/v1/applications/{applicationID}/ignore-selectors/{selectorID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.deleteIgnoreSelector))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/resources", s.requireAuth(http.HandlerFunc(s.listApplicationResources)))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/topology", s.requireAuth(http.HandlerFunc(s.applicationTopology)))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/operations", s.requireAuth(http.HandlerFunc(s.listApplicationOperations)))
	s.Mux.Handle("GET /api/v1/plans/{planID}", s.requireAuth(http.HandlerFunc(s.getPlan)))
	s.Mux.Handle("POST /api/v1/plans/{planID}/approvals", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.approvePlan))))
	s.Mux.Handle("GET /api/v1/plans/{planID}/approvals", s.requireAuth(http.HandlerFunc(s.listPlanApprovals)))
	s.Mux.Handle("POST /api/v1/plans/{planID}/selections", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createPlanSelection))))
	s.Mux.Handle("POST /api/v1/plans/{planID}/apply", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.applyPlan))))
	s.Mux.Handle("GET /api/v1/audit", s.requireAuth(http.HandlerFunc(s.listAudit)))
	s.Mux.Handle("GET /api/v1/admin/users", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.listUsers))))
	s.Mux.Handle("POST /api/v1/admin/users", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.createUser)))))
	s.Mux.Handle("PUT /api/v1/admin/users/{userID}", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.updateUser)))))
	s.Mux.Handle("DELETE /api/v1/admin/users/{userID}", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.deleteUser)))))
	s.Mux.Handle("GET /api/v1/admin/oidc-providers", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.listOIDCProviders))))
	s.Mux.Handle("POST /api/v1/admin/oidc-providers", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.createOIDCProvider)))))
	s.Mux.Handle("POST /api/v1/admin/oidc-providers/{providerID}/groups", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.addOIDCGroupRole)))))

	s.Mux.HandleFunc("GET /api/v1/openapi.yaml", s.serveOpenAPI)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	}
	s.Mux.ServeHTTP(w, r)
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		session, err := s.Store.SessionByTokenHash(r.Context(), security.HashToken(cookie.Value))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		user, err := s.Store.UserByID(r.Context(), session.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		session, err := s.Store.SessionByTokenHash(r.Context(), security.HashToken(cookie.Value))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		csrfCookie, err := r.Cookie(csrfCookieName)
		header := r.Header.Get("X-CSRF-Token")
		if err != nil || csrfCookie.Value == "" || header == "" || subtle.ConstantTimeCompare([]byte(csrfCookie.Value), []byte(header)) != 1 || subtle.ConstantTimeCompare(session.CSRFHash, security.HashToken(header)) != 1 {
			writeError(w, http.StatusForbidden, "CSRF validation failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if !u.IsAdmin {
			writeError(w, http.StatusForbidden, "instance administrator role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireProjectRole(w http.ResponseWriter, r *http.Request, projectID string, minimum string) bool {
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectId is required")
		return false
	}
	role, err := s.Store.ProjectRole(r.Context(), currentUser(r), projectID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			writeError(w, http.StatusRequestTimeout, "request cancelled")
		} else {
			writeError(w, http.StatusForbidden, "project access denied")
		}
		return false
	}
	allowed := role == "owner" || (minimum != "owner" && role == "deployer") || (minimum == "viewer" && role == "viewer")
	if !allowed {
		writeError(w, http.StatusForbidden, "project role is not permitted")
		return false
	}
	return true
}

func currentUser(r *http.Request) store.User {
	user, _ := r.Context().Value(userContextKey).(store.User)
	return user
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	err := decoder.Decode(&extra)
	if err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListProjects(r.Context(), currentUser(r))
	if err != nil {
		writeStoreError(w, "could not load projects")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 {
		writeError(w, http.StatusBadRequest, "project name is required and must be at most 100 characters")
		return
	}
	project := store.Project{ID: store.NewID(), Name: input.Name, Description: strings.TrimSpace(input.Description), ApprovalPolicy: store.DefaultApprovalPolicy()}
	user := currentUser(r)
	if err := s.Store.CreateProject(r.Context(), project, user.ID); err != nil {
		writeError(w, http.StatusConflict, "could not create project")
		return
	}
	_ = s.Store.Audit(r.Context(), user.ID, "project.created", "project", project.ID, map[string]string{"name": project.Name})
	project.Role = "owner"
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !user.IsAdmin {
		writeError(w, http.StatusForbidden, "instance administrator role required")
		return
	}
	before := int64(0)
	if raw := r.URL.Query().Get("before"); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "before must be a positive audit event ID")
			return
		}
		before = parsed
	}
	items, err := s.Store.ListAuditEvents(r.Context(), 51, before)
	if err != nil {
		writeStoreError(w, "could not load audit events")
		return
	}
	hasMore := len(items) > 50
	if hasMore {
		items = items[:50]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListUsers(r.Context())
	if err != nil {
		writeStoreError(w, "could not load users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		IsAdmin     bool   `json:"isAdmin"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Email = strings.TrimSpace(strings.ToLower(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if !strings.Contains(input.Email, "@") || len(input.Email) > 320 {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(input.DisplayName) > 200 {
		writeError(w, http.StatusBadRequest, "display name must be 200 characters or fewer")
		return
	}
	hash, err := security.HashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := store.User{ID: store.NewID(), Email: input.Email, DisplayName: strings.TrimSpace(input.DisplayName), IsAdmin: input.IsAdmin}
	u, err = s.Store.CreateUser(r.Context(), u, hash)
	if err != nil {
		writeError(w, http.StatusConflict, "could not create user; the email may already be in use")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "user.created", "user", u.ID, map[string]any{"email": u.Email, "isAdmin": u.IsAdmin})
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		IsAdmin     bool   `json:"isAdmin"`
		Disabled    bool   `json:"disabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Email = strings.TrimSpace(strings.ToLower(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if !strings.Contains(input.Email, "@") || len(input.Email) > 320 {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(input.DisplayName) > 200 {
		writeError(w, http.StatusBadRequest, "display name must be 200 characters or fewer")
		return
	}
	passwordHash := ""
	if input.Password != "" {
		var err error
		passwordHash, err = security.HashPassword(input.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	userID := r.PathValue("userID")
	if userID == currentUser(r).ID && (!input.IsAdmin || input.Disabled) {
		writeError(w, http.StatusConflict, "you cannot lock or demote your own account")
		return
	}
	user, err := s.Store.UpdateAdminUser(r.Context(), userID, input.Email, input.DisplayName, input.IsAdmin, input.Disabled, passwordHash)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "user.updated", "user", user.ID, map[string]any{"email": user.Email, "displayName": user.DisplayName, "isAdmin": user.IsAdmin, "disabled": user.Disabled, "passwordChanged": passwordHash != ""})
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if userID == currentUser(r).ID {
		writeError(w, http.StatusConflict, "you cannot delete your own account")
		return
	}
	if err := s.Store.DeleteAdminUser(r.Context(), userID); err != nil {
		writeAdminUserError(w, err)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "user.deleted", "user", userID, map[string]bool{"anonymized": true})
	w.WriteHeader(http.StatusNoContent)
}

func writeAdminUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, store.ErrLastInstanceAdmin), errors.Is(err, store.ErrUserLastProjectOwner), errors.Is(err, store.ErrUserDeleted), errors.Is(err, store.ErrSystemUser):
		writeError(w, http.StatusConflict, err.Error())
	default:
		var sqlStateErr interface{ SQLState() string }
		if errors.As(err, &sqlStateErr) && sqlStateErr.SQLState() == "23505" {
			writeError(w, http.StatusConflict, "that email address is already in use")
			return
		}
		writeStoreError(w, "could not update user")
	}
}

func (s *Server) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = io.WriteString(w, openAPISpec)
}

var _ http.Handler = (*Server)(nil)
