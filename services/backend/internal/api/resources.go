package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?$`)
var namespaceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func (s *Server) listProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	if !s.requireProjectRole(w, r, projectID, "viewer") {
		return
	}
	items, err := s.Store.ListProjectMembers(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load project members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) setProjectMember(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	if !s.requireProjectRole(w, r, projectID, "owner") {
		return
	}
	var input struct {
		UserID string `json:"userId"`
		Email  string `json:"email"`
		Role   string `json:"role"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.UserID == "" && strings.TrimSpace(input.Email) != "" {
		user, _, err := s.Store.UserByEmail(r.Context(), strings.TrimSpace(strings.ToLower(input.Email)))
		if err != nil {
			writeError(w, http.StatusBadRequest, "no active JustCD user has that email address")
			return
		}
		input.UserID = user.ID
	}
	if input.UserID == "" || !validRole(input.Role) {
		writeError(w, http.StatusBadRequest, "an active user and a valid project role are required")
		return
	}
	if input.UserID == currentUser(r).ID && input.Role != "owner" {
		writeError(w, http.StatusConflict, "you cannot demote yourself from project owner")
		return
	}
	if input.Role != "owner" {
		previousRole, roleErr := s.Store.ProjectRoleForUser(r.Context(), projectID, input.UserID)
		if roleErr != nil {
			writeError(w, http.StatusInternalServerError, "could not verify project ownership")
			return
		}
		if previousRole == "owner" {
			owners, countErr := s.Store.ProjectOwnerCount(r.Context(), projectID)
			if countErr != nil {
				writeError(w, http.StatusInternalServerError, "could not verify project ownership")
				return
			}
			if owners <= 1 {
				writeError(w, http.StatusConflict, "a project must keep at least one owner")
				return
			}
		}
	}
	if err := s.Store.SetProjectMember(r.Context(), projectID, input.UserID, input.Role); err != nil {
		writeError(w, http.StatusBadRequest, "could not set project member")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "project_member.updated", "project", projectID, map[string]string{"userId": input.UserID, "role": input.Role})
	writeJSON(w, http.StatusOK, map[string]string{"projectId": projectID, "userId": input.UserID, "role": input.Role})
}
func (s *Server) removeProjectMember(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	if !s.requireProjectRole(w, r, projectID, "owner") {
		return
	}
	userID := r.PathValue("userID")
	if userID == currentUser(r).ID {
		writeError(w, http.StatusBadRequest, "you cannot remove yourself from the project")
		return
	}
	role, roleErr := s.Store.ProjectRoleForUser(r.Context(), projectID, userID)
	if roleErr != nil {
		writeError(w, http.StatusInternalServerError, "could not verify project ownership")
		return
	}
	if role == "owner" {
		owners, countErr := s.Store.ProjectOwnerCount(r.Context(), projectID)
		if countErr != nil {
			writeError(w, http.StatusInternalServerError, "could not verify project ownership")
			return
		}
		if owners <= 1 {
			writeError(w, http.StatusConflict, "a project must keep at least one owner")
			return
		}
	}
	if err := s.Store.RemoveProjectMember(r.Context(), projectID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove project member")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "project_member.removed", "project", projectID, map[string]string{"userId": userID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if !s.requireProjectRole(w, r, projectID, "viewer") {
		return
	}
	items, err := s.Store.ListCredentials(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID *string           `json:"projectId"`
		Name      string            `json:"name"`
		Kind      string            `json:"kind"`
		ExpiresAt *string           `json:"expiresAt"`
		Secret    map[string]string `json:"secret"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := currentUser(r)
	if input.ProjectID != nil {
		if !s.requireProjectRole(w, r, *input.ProjectID, "owner") {
			return
		}
	} else if !user.IsAdmin {
		writeError(w, http.StatusForbidden, "instance administrator role required for global credentials")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 {
		writeError(w, http.StatusBadRequest, "credential name is required and must be at most 100 characters")
		return
	}
	if err := validateCredentialPayload(input.Kind, input.Secret); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := json.Marshal(input.Secret)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid credential payload")
		return
	}
	credential := store.Credential{ID: store.NewID(), ProjectID: input.ProjectID, Name: input.Name, Kind: input.Kind}
	if input.ExpiresAt != nil {
		value := strings.TrimSpace(*input.ExpiresAt)
		if value != "" {
			parsed, err := timeParse(value)
			if err != nil || !parsed.After(time.Now()) {
				writeError(w, http.StatusBadRequest, "expiresAt must be a future RFC3339 timestamp")
				return
			}
			credential.ExpiresAt = &parsed
		}
	}
	credential.Cipher, err = security.Encrypt(s.EncryptionKey, payload, "credential:"+credential.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encrypt credential")
		return
	}
	if err := s.Store.CreateCredential(r.Context(), credential); err != nil {
		writeError(w, http.StatusConflict, "could not save credential")
		return
	}
	_ = s.Store.Audit(r.Context(), user.ID, "credential.created", "credential", credential.ID, map[string]string{"name": credential.Name, "kind": credential.Kind})
	credential.Cipher = nil
	writeJSON(w, http.StatusCreated, credential)
}

func validateCredentialPayload(kind string, secret map[string]string) error {
	allowOnly := func(keys ...string) error {
		allowed := make(map[string]bool, len(keys))
		for _, key := range keys {
			allowed[key] = true
		}
		for key := range secret {
			if !allowed[key] {
				return fmtError("secret.%s is not supported for this credential type", key)
			}
		}
		return nil
	}
	required := func(key string) error {
		if strings.TrimSpace(secret[key]) == "" {
			return fmtError("secret.%s is required", key)
		}
		return nil
	}
	switch kind {
	case "git-ssh":
		if err := allowOnly("privateKey", "knownHosts"); err != nil {
			return err
		}
		if err := required("privateKey"); err != nil {
			return err
		}
		if err := required("knownHosts"); err != nil {
			return err
		}
		if len(secret["privateKey"]) > 64<<10 || len(secret["knownHosts"]) > 64<<10 {
			return fmtError("SSH credential fields must each be at most 64 KiB")
		}
		return nil
	case "git-https":
		if err := allowOnly("token", "username"); err != nil {
			return err
		}
		if err := required("token"); err != nil {
			return err
		}
		if len(secret["token"]) > 64<<10 {
			return fmtError("Git token exceeds 64 KiB")
		}
		return nil
	case "kubernetes-token":
		if err := allowOnly("token"); err != nil {
			return err
		}
		if err := required("token"); err != nil {
			return err
		}
		if len(secret["token"]) > 64<<10 {
			return fmtError("Kubernetes token exceeds 64 KiB")
		}
		return nil
	case "kubeconfig":
		if err := allowOnly("content"); err != nil {
			return err
		}
		if err := required("content"); err != nil {
			return err
		}
		if len(secret["content"]) > 1<<20 {
			return fmtError("kubeconfig content exceeds 1 MiB")
		}
		return nil
	default:
		return fmtError("kind must be git-ssh, git-https, kubernetes-token, or kubeconfig")
	}
}

func (s *Server) listClusters(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListClusters(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load clusters")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name                     string  `json:"name"`
		APIServer                string  `json:"apiServer"`
		CADataBase64             string  `json:"caDataBase64"`
		InsecureSkipVerify       bool    `json:"insecureSkipVerify"`
		DefaultCredentialID      *string `json:"defaultCredentialId"`
		ClusterScopeCredentialID *string `json:"clusterScopeCredentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	endpoint, err := url.Parse(strings.TrimSpace(input.APIServer))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		writeError(w, http.StatusBadRequest, "apiServer must be an HTTPS Kubernetes API URL")
		return
	}
	if input.Name == "" || len(input.Name) > 100 {
		writeError(w, http.StatusBadRequest, "cluster name is required and must be at most 100 characters")
		return
	}
	caData, err := base64.StdEncoding.DecodeString(input.CADataBase64)
	if err != nil && input.CADataBase64 != "" {
		writeError(w, http.StatusBadRequest, "caDataBase64 must be valid base64")
		return
	}
	if len(caData) > 128<<10 {
		writeError(w, http.StatusBadRequest, "cluster CA data exceeds 128 KiB")
		return
	}
	for _, credentialID := range []*string{input.DefaultCredentialID, input.ClusterScopeCredentialID} {
		if credentialID != nil {
			c, err := s.Store.CredentialByID(r.Context(), *credentialID)
			if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || c.ProjectID != nil {
				writeError(w, http.StatusBadRequest, "cluster credentials must be global Kubernetes credentials")
				return
			}
		}
	}
	cluster := store.Cluster{ID: store.NewID(), Name: input.Name, APIServer: endpoint.String(), CAData: caData, InsecureSkipVerify: input.InsecureSkipVerify, DefaultCredentialID: input.DefaultCredentialID, ClusterScopeCredential: input.ClusterScopeCredentialID}
	if err := s.Store.CreateCluster(r.Context(), cluster); err != nil {
		writeError(w, http.StatusConflict, "could not create cluster")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "cluster.created", "cluster", cluster.ID, map[string]string{"name": cluster.Name, "apiServer": cluster.APIServer})
	cluster.CAData = nil
	writeJSON(w, http.StatusCreated, cluster)
}

func (s *Server) createNamespaceBinding(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID    string  `json:"projectId"`
		Namespace    string  `json:"namespace"`
		CredentialID *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireProjectRole(w, r, input.ProjectID, "owner") {
		return
	}
	input.Namespace = strings.TrimSpace(input.Namespace)
	if !namespaceNamePattern.MatchString(input.Namespace) {
		writeError(w, http.StatusBadRequest, "namespace is required")
		return
	}
	cluster, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	if input.CredentialID != nil {
		c, err := s.Store.CredentialByID(r.Context(), *input.CredentialID)
		if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || (c.ProjectID != nil && *c.ProjectID != input.ProjectID) {
			writeError(w, http.StatusBadRequest, "credential must be a Kubernetes credential available to this project")
			return
		}
	}
	if input.CredentialID == nil && cluster.DefaultCredentialID == nil {
		writeError(w, http.StatusBadRequest, "a namespace credential or cluster default credential is required")
		return
	}
	if err := s.Store.CreateNamespaceBinding(r.Context(), input.ProjectID, cluster.ID, input.Namespace, input.CredentialID); err != nil {
		writeError(w, http.StatusConflict, "could not create namespace binding")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "namespace_binding.created", "cluster", cluster.ID, map[string]string{"projectId": input.ProjectID, "namespace": input.Namespace, "credentialId": valueOf(input.CredentialID)})
	writeJSON(w, http.StatusCreated, store.NamespaceBinding{Namespace: input.Namespace, CredentialID: input.CredentialID})
}

func (s *Server) listNamespaceBindings(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if !s.requireProjectRole(w, r, projectID, "viewer") {
		return
	}
	items, err := s.Store.ListNamespaceBindings(r.Context(), projectID, r.PathValue("clusterID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load namespace bindings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listGitSources(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if !s.requireProjectRole(w, r, projectID, "viewer") {
		return
	}
	items, err := s.Store.ListGitSources(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load Git sources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createGitSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID     string  `json:"projectId"`
		Name          string  `json:"name"`
		RepositoryURL string  `json:"repositoryUrl"`
		CredentialID  *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireProjectRole(w, r, input.ProjectID, "owner") {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	if input.Name == "" || len(input.Name) > 100 || !validGitURL(input.RepositoryURL) {
		writeError(w, http.StatusBadRequest, "name and a supported SSH or HTTPS Git URL are required")
		return
	}
	if input.CredentialID != nil {
		c, err := s.Store.CredentialByID(r.Context(), *input.CredentialID)
		if err != nil || (c.ProjectID != nil && *c.ProjectID != input.ProjectID) || (c.Kind != "git-ssh" && c.Kind != "git-https") {
			writeError(w, http.StatusBadRequest, "credential must be a Git credential available to this project")
			return
		}
	}
	source := store.GitSource{ID: store.NewID(), ProjectID: input.ProjectID, Name: input.Name, RepositoryURL: input.RepositoryURL, CredentialID: input.CredentialID}
	if err := s.Store.CreateGitSource(r.Context(), source); err != nil {
		writeError(w, http.StatusConflict, "could not create Git source")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "git_source.created", "git_source", source.ID, map[string]string{"projectId": source.ProjectID, "name": source.Name})
	writeJSON(w, http.StatusCreated, source)
}

func validGitURL(value string) bool {
	if strings.ContainsAny(value, " \t\r\n\x00") {
		return false
	}
	if strings.HasPrefix(value, "git@") {
		hostAndPath, ok := strings.CutPrefix(value, "git@")
		if !ok {
			return false
		}
		host, repository, hasPath := strings.Cut(hostAndPath, ":")
		return hasPath && host != "" && repository != "" && !strings.ContainsAny(host, "/@?#") && !strings.ContainsAny(repository, "?#")
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return u.User == nil && u.Path != ""
	}
	if u.Scheme != "ssh" || u.Path == "" {
		return false
	}
	if u.User != nil {
		_, hasPassword := u.User.Password()
		if u.User.Username() != "git" || hasPassword {
			return false
		}
	}
	return true
}

func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if !s.requireProjectRole(w, r, projectID, "viewer") {
		return
	}
	items, err := s.Store.ListApplications(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load applications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID    string   `json:"projectId"`
		Name         string   `json:"name"`
		SourceID     string   `json:"sourceId"`
		Revision     string   `json:"revision"`
		ManifestPath string   `json:"manifestPath"`
		Renderer     string   `json:"renderer"`
		ClusterID    string   `json:"clusterId"`
		Namespaces   []string `json:"namespaces"`
		SyncPolicy   string   `json:"syncPolicy"`
		PollSeconds  int      `json:"pollSeconds"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireProjectRole(w, r, input.ProjectID, "owner") {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !resourceNamePattern.MatchString(input.Name) {
		writeError(w, http.StatusBadRequest, "application name must be a lowercase Kubernetes-style name")
		return
	}
	if input.Revision == "" || len(input.Revision) > 256 {
		writeError(w, http.StatusBadRequest, "a Git branch, tag, or commit revision is required")
		return
	}
	input.ManifestPath = path.Clean(strings.TrimSpace(input.ManifestPath))
	if input.ManifestPath == "." || strings.HasPrefix(input.ManifestPath, "../") || path.IsAbs(input.ManifestPath) {
		writeError(w, http.StatusBadRequest, "manifestPath must be a repository-relative path")
		return
	}
	if input.Renderer != "yaml" && input.Renderer != "kustomize" && input.Renderer != "helm" {
		writeError(w, http.StatusBadRequest, "renderer must be yaml, kustomize, or helm")
		return
	}
	if input.SyncPolicy == "" {
		input.SyncPolicy = "manual"
	}
	if input.SyncPolicy != "manual" && input.SyncPolicy != "auto-safe" {
		writeError(w, http.StatusBadRequest, "syncPolicy must be manual or auto-safe")
		return
	}
	if input.PollSeconds == 0 {
		input.PollSeconds = 300
	}
	if input.PollSeconds < 30 || input.PollSeconds > 86400 {
		writeError(w, http.StatusBadRequest, "pollSeconds must be between 30 and 86400")
		return
	}
	source, err := s.Store.GitSourceByID(r.Context(), input.SourceID)
	if err != nil || source.ProjectID != input.ProjectID {
		writeError(w, http.StatusBadRequest, "Git source is not available to this project")
		return
	}
	if _, err := s.Store.ClusterByID(r.Context(), input.ClusterID); err != nil {
		writeError(w, http.StatusBadRequest, "cluster not found")
		return
	}
	if len(input.Namespaces) == 0 {
		writeError(w, http.StatusBadRequest, "at least one namespace binding is required")
		return
	}
	if input.Renderer == "helm" && len(input.Namespaces) != 1 {
		writeError(w, http.StatusBadRequest, "Helm applications require exactly one namespace binding")
		return
	}
	bindings := make([]store.NamespaceBinding, 0, len(input.Namespaces))
	seen := map[string]bool{}
	for _, namespace := range input.Namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" || seen[namespace] {
			writeError(w, http.StatusBadRequest, "namespace names must be nonempty and unique")
			return
		}
		seen[namespace] = true
		binding, err := s.Store.NamespaceBinding(r.Context(), input.ProjectID, input.ClusterID, namespace)
		if err != nil {
			writeError(w, http.StatusBadRequest, "every application namespace must have a project binding")
			return
		}
		bindings = append(bindings, binding)
	}
	app := store.Application{ID: store.NewID(), ProjectID: input.ProjectID, Name: input.Name, SourceID: input.SourceID, Revision: input.Revision, ManifestPath: input.ManifestPath, Renderer: input.Renderer, ClusterID: input.ClusterID, Namespaces: bindings, SyncPolicy: input.SyncPolicy, PollSeconds: input.PollSeconds, Health: "unknown"}
	if err := s.Store.CreateApplication(r.Context(), app); err != nil {
		writeError(w, http.StatusConflict, "could not create application")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.created", "application", app.ID, map[string]any{"projectId": app.ProjectID, "sourceId": app.SourceID, "clusterId": app.ClusterID, "renderer": app.Renderer})
	writeJSON(w, http.StatusCreated, app)
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
