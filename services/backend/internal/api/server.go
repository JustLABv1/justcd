package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
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
	s.Mux.Handle("GET /api/v1/projects/{projectID}/members", s.requireAuth(http.HandlerFunc(s.listProjectMembers)))
	s.Mux.Handle("POST /api/v1/projects/{projectID}/members", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.setProjectMember))))
	s.Mux.Handle("DELETE /api/v1/projects/{projectID}/members/{userID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.removeProjectMember))))
	s.Mux.Handle("GET /api/v1/credentials", s.requireAuth(http.HandlerFunc(s.listCredentials)))
	s.Mux.Handle("POST /api/v1/credentials", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createCredential))))
	s.Mux.Handle("GET /api/v1/clusters", s.requireAuth(http.HandlerFunc(s.listClusters)))
	s.Mux.Handle("POST /api/v1/clusters", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.createCluster)))))
	s.Mux.Handle("POST /api/v1/clusters/{clusterID}/bindings", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createNamespaceBinding))))
	s.Mux.Handle("GET /api/v1/clusters/{clusterID}/bindings", s.requireAuth(http.HandlerFunc(s.listNamespaceBindings)))
	s.Mux.Handle("GET /api/v1/git-sources", s.requireAuth(http.HandlerFunc(s.listGitSources)))
	s.Mux.Handle("POST /api/v1/git-sources", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createGitSource))))
	s.Mux.Handle("GET /api/v1/applications", s.requireAuth(http.HandlerFunc(s.listApplications)))
	s.Mux.Handle("POST /api/v1/applications", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createApplication))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}", s.requireAuth(http.HandlerFunc(s.getApplication)))
	s.Mux.Handle("POST /api/v1/applications/{applicationID}/plans", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createPlan))))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/plans", s.requireAuth(http.HandlerFunc(s.listPlans)))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/resources", s.requireAuth(http.HandlerFunc(s.listApplicationResources)))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/topology", s.requireAuth(http.HandlerFunc(s.applicationTopology)))
	s.Mux.Handle("GET /api/v1/applications/{applicationID}/operations", s.requireAuth(http.HandlerFunc(s.listApplicationOperations)))
	s.Mux.Handle("GET /api/v1/plans/{planID}", s.requireAuth(http.HandlerFunc(s.getPlan)))
	s.Mux.Handle("POST /api/v1/plans/{planID}/approvals", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.approvePlan))))
	s.Mux.Handle("POST /api/v1/plans/{planID}/apply", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.applyPlan))))
	s.Mux.Handle("GET /api/v1/audit", s.requireAuth(http.HandlerFunc(s.listAudit)))
	s.Mux.Handle("GET /api/v1/admin/users", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.listUsers))))
	s.Mux.Handle("POST /api/v1/admin/users", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.createUser)))))
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
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListProjects(r.Context(), currentUser(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load projects")
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
	project := store.Project{ID: store.NewID(), Name: input.Name, Description: strings.TrimSpace(input.Description)}
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
	items, err := s.Store.ListAuditEvents(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load audit events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load users")
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
	if !strings.Contains(input.Email, "@") || len(input.Email) > 320 {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	hash, err := security.HashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := store.User{ID: store.NewID(), Email: input.Email, DisplayName: strings.TrimSpace(input.DisplayName), IsAdmin: input.IsAdmin}
	if err := s.Store.CreateUser(r.Context(), u, hash); err != nil {
		writeError(w, http.StatusConflict, "could not create user; the email may already be in use")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "user.created", "user", u.ID, map[string]any{"email": u.Email, "isAdmin": u.IsAdmin})
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = io.WriteString(w, openAPISpec)
}

var _ http.Handler = (*Server)(nil)
