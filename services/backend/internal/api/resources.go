package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?$`)
var namespaceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validateHelmValuesInput(renderer string, files []string, valuesYAML string) ([]string, string, error) {
	return store.ValidateHelmValuesInput(renderer, files, valuesYAML)
}

func validateNamespaceHelmValuesInput(renderer string, namespaceValues map[string]core.HelmValuesOverride, namespaces []string) (map[string]core.HelmValuesOverride, error) {
	if len(namespaceValues) == 0 {
		return map[string]core.HelmValuesOverride{}, nil
	}
	if renderer != "helm" {
		return nil, errors.New("namespace-specific values require the Helm renderer")
	}
	if len(namespaceValues) > 100 {
		return nil, errors.New("namespace-specific values exceed 100 entries")
	}
	bound := make(map[string]bool, len(namespaces))
	for _, namespace := range namespaces {
		bound[strings.TrimSpace(namespace)] = true
	}
	validated := make(map[string]core.HelmValuesOverride, len(namespaceValues))
	for namespace, values := range namespaceValues {
		namespace = strings.TrimSpace(namespace)
		if !namespaceNamePattern.MatchString(namespace) || !bound[namespace] {
			return nil, fmt.Errorf("namespace-specific values require a selected namespace; %q is not bound to this application", namespace)
		}
		files, valuesYAML, err := validateHelmValuesInput(renderer, values.Files, values.YAML)
		if err != nil {
			return nil, fmt.Errorf("namespace %q: %w", namespace, err)
		}
		validated[namespace] = core.HelmValuesOverride{Files: files, YAML: valuesYAML}
	}
	return validated, nil
}

func validateKustomizeManifestPaths(renderer, targetPath string, namespacePaths map[string]string, namespaces []string) (string, map[string]string, error) {
	cleanPath := func(value string) (string, error) {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", nil
		}
		clean := path.Clean(value)
		if strings.ContainsRune(value, '\x00') || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return "", errors.New("Kustomize paths must be repository-relative")
		}
		return clean, nil
	}
	targetPath, err := cleanPath(targetPath)
	if err != nil {
		return "", nil, err
	}
	if renderer != "kustomize" {
		if targetPath != "" || len(namespacePaths) > 0 {
			return "", nil, errors.New("Kustomize overlay paths require the Kustomize renderer")
		}
		return "", map[string]string{}, nil
	}
	if len(namespacePaths) > 100 {
		return "", nil, errors.New("namespace Kustomize paths exceed 100 entries")
	}
	bound := make(map[string]bool, len(namespaces))
	for _, namespace := range namespaces {
		bound[strings.TrimSpace(namespace)] = true
	}
	validated := make(map[string]string, len(namespacePaths))
	seen := make(map[string]bool, len(namespacePaths))
	for namespace, value := range namespacePaths {
		namespace = strings.TrimSpace(namespace)
		if !namespaceNamePattern.MatchString(namespace) || !bound[namespace] {
			return "", nil, fmt.Errorf("namespace Kustomize paths require a selected namespace; %q is not bound to this application", namespace)
		}
		if seen[namespace] {
			return "", nil, fmt.Errorf("namespace Kustomize paths contain duplicate entries for %q", namespace)
		}
		seen[namespace] = true
		clean, err := cleanPath(value)
		if err != nil {
			return "", nil, fmt.Errorf("namespace %q: %w", namespace, err)
		}
		if clean != "" {
			validated[namespace] = clean
		}
	}
	return targetPath, validated, nil
}

func (s *Server) listWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListWorkspaceMembers(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load workspace members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) setWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "owner") {
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
		writeError(w, http.StatusBadRequest, "an active user and a valid workspace role are required")
		return
	}
	if input.UserID == currentUser(r).ID && input.Role != "owner" {
		writeError(w, http.StatusConflict, "you cannot demote yourself from workspace owner")
		return
	}
	if err := s.Store.SetWorkspaceMember(r.Context(), workspaceID, input.UserID, input.Role); err != nil {
		writeWorkspaceMemberError(w, err)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_member.updated", "workspace", workspaceID, map[string]string{"userId": input.UserID, "role": input.Role})
	writeJSON(w, http.StatusOK, map[string]string{"workspaceId": workspaceID, "userId": input.UserID, "role": input.Role})
}

func (s *Server) updateWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "owner") {
		return
	}
	userID := r.PathValue("userID")
	var input struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validRole(input.Role) {
		writeError(w, http.StatusBadRequest, "a valid workspace role is required")
		return
	}
	if userID == currentUser(r).ID && input.Role != "owner" {
		writeError(w, http.StatusConflict, "you cannot demote yourself from workspace owner")
		return
	}
	if err := s.Store.SetWorkspaceMember(r.Context(), workspaceID, userID, input.Role); err != nil {
		writeWorkspaceMemberError(w, err)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_member.updated", "workspace", workspaceID, map[string]string{"userId": userID, "role": input.Role})
	writeJSON(w, http.StatusOK, map[string]string{"workspaceId": workspaceID, "userId": userID, "role": input.Role})
}

func (s *Server) removeWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "owner") {
		return
	}
	userID := r.PathValue("userID")
	if userID == currentUser(r).ID {
		writeError(w, http.StatusBadRequest, "you cannot remove yourself from the workspace")
		return
	}
	if err := s.Store.RemoveWorkspaceMember(r.Context(), workspaceID, userID); err != nil {
		writeWorkspaceMemberError(w, err)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_member.removed", "workspace", workspaceID, map[string]string{"userId": userID})
	w.WriteHeader(http.StatusNoContent)
}

func writeWorkspaceMemberError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrWorkspaceNotFound), errors.Is(err, store.ErrWorkspaceMemberNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrLastWorkspaceOwner), errors.Is(err, store.ErrWorkspaceMemberManagedSSO):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrUserUnavailable):
		writeError(w, http.StatusBadRequest, "locked or deleted users cannot be added to a workspace")
	default:
		writeStoreError(w, "could not update workspace member")
	}
}

func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListCredentials(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load credentials")
		return
	}
	for i := range items {
		if items[i].Kind != "git-https" {
			continue
		}
		items[i].Username, err = s.gitHTTPSUsername(items[i])
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read Git credential username")
			return
		}
		items[i].Cipher = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID *string           `json:"workspaceId"`
		Name        string            `json:"name"`
		Kind        string            `json:"kind"`
		ExpiresAt   *string           `json:"expiresAt"`
		Secret      map[string]string `json:"secret"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := currentUser(r)
	if input.WorkspaceID != nil {
		if !s.requireWorkspaceRole(w, r, *input.WorkspaceID, "owner") {
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
	credential := store.Credential{ID: store.NewID(), WorkspaceID: input.WorkspaceID, Name: input.Name, Kind: input.Kind}
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
	if credential.Kind == "git-https" {
		credential.Username = strings.TrimSpace(input.Secret["username"])
		if credential.Username == "" {
			credential.Username = "justcd"
		}
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
		if len(secret["username"]) > 256 || strings.ContainsAny(secret["username"], "\r\n\x00") {
			return fmtError("Git username is invalid")
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
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListClusters(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load clusters")
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
		MaxConcurrentOperations  *int    `json:"maxConcurrentOperations"`
		OperationsPerMinute      *int    `json:"operationsPerMinute"`
		WorkspaceID              string  `json:"workspaceId"`
		WorkspaceCredentialID    *string `json:"workspaceCredentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
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
	maxConcurrentOperations, operationsPerMinute := 2, 30
	if input.MaxConcurrentOperations != nil && *input.MaxConcurrentOperations != 0 {
		maxConcurrentOperations = *input.MaxConcurrentOperations
	}
	if input.OperationsPerMinute != nil && *input.OperationsPerMinute != 0 {
		operationsPerMinute = *input.OperationsPerMinute
	}
	if !validClusterOperationLimits(maxConcurrentOperations, operationsPerMinute) {
		writeError(w, http.StatusBadRequest, "cluster operation limits must allow 1–20 concurrent operations and 1–1000 operations per minute")
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
			writeError(w, http.StatusBadRequest, "new workspace clusters must use a workspace-owned Kubernetes credential")
			return
		}
	}
	if input.WorkspaceCredentialID != nil {
		if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
			return
		}
		c, err := s.Store.CredentialByID(r.Context(), *input.WorkspaceCredentialID)
		if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || c.WorkspaceID == nil || *c.WorkspaceID != input.WorkspaceID {
			writeError(w, http.StatusBadRequest, "workspace credential must be a Kubernetes credential owned by the selected workspace")
			return
		}
	}
	cluster := store.Cluster{ID: store.NewID(), WorkspaceID: &input.WorkspaceID, Name: input.Name, APIServer: endpoint.String(), CAData: caData, InsecureSkipVerify: input.InsecureSkipVerify, DefaultCredentialID: input.DefaultCredentialID, ClusterScopeCredential: input.ClusterScopeCredentialID, MaxConcurrentOperations: maxConcurrentOperations, OperationsPerMinute: operationsPerMinute}
	var createErr error
	if input.WorkspaceCredentialID != nil {
		createErr = s.Store.CreateClusterWithWorkspaceCredential(r.Context(), cluster, input.WorkspaceID, *input.WorkspaceCredentialID)
	} else {
		createErr = s.Store.CreateWorkspaceCluster(r.Context(), cluster)
	}
	if createErr != nil {
		writeError(w, http.StatusConflict, "could not create cluster")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "cluster.created", "cluster", cluster.ID, map[string]string{"name": cluster.Name, "apiServer": cluster.APIServer})
	cluster.CAData = nil
	writeJSON(w, http.StatusCreated, cluster)
}

func (s *Server) createNamespaceBinding(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID  string  `json:"workspaceId"`
		Namespace    string  `json:"namespace"`
		CredentialID *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
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
	if !s.requireWorkspaceCluster(w, r, input.WorkspaceID, cluster.ID) {
		return
	}
	if input.CredentialID != nil {
		c, err := s.Store.CredentialByID(r.Context(), *input.CredentialID)
		if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || (c.WorkspaceID != nil && *c.WorkspaceID != input.WorkspaceID) {
			writeError(w, http.StatusBadRequest, "credential must be a Kubernetes credential available to this workspace")
			return
		}
	}
	workspaceCredentialID, err := s.Store.WorkspaceClusterCredential(r.Context(), input.WorkspaceID, cluster.ID)
	if err != nil {
		writeStoreError(w, "could not load workspace cluster credential")
		return
	}
	if input.CredentialID == nil && workspaceCredentialID == nil && cluster.DefaultCredentialID == nil {
		writeError(w, http.StatusBadRequest, "a namespace credential or cluster default credential is required")
		return
	}
	if err := s.Store.CreateNamespaceBinding(r.Context(), input.WorkspaceID, cluster.ID, input.Namespace, input.CredentialID); err != nil {
		writeError(w, http.StatusConflict, "could not create namespace binding")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "namespace_binding.created", "cluster", cluster.ID, map[string]string{"workspaceId": input.WorkspaceID, "namespace": input.Namespace, "credentialId": valueOf(input.CredentialID)})
	writeJSON(w, http.StatusCreated, store.NamespaceBinding{Namespace: input.Namespace, CredentialID: input.CredentialID})
}

func (s *Server) getWorkspaceClusterCredential(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	if !s.requireWorkspaceCluster(w, r, workspaceID, r.PathValue("clusterID")) {
		return
	}
	if _, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID")); err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	id, err := s.Store.WorkspaceClusterCredential(r.Context(), workspaceID, r.PathValue("clusterID"))
	if err != nil {
		writeStoreError(w, "could not load workspace cluster credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentialId": id})
}

func (s *Server) setWorkspaceClusterCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID  string  `json:"workspaceId"`
		CredentialID *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
		return
	}
	if !s.requireWorkspaceCluster(w, r, input.WorkspaceID, r.PathValue("clusterID")) {
		return
	}
	if _, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID")); err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	if input.CredentialID != nil {
		c, err := s.Store.CredentialByID(r.Context(), *input.CredentialID)
		if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || c.WorkspaceID == nil || *c.WorkspaceID != input.WorkspaceID {
			writeError(w, http.StatusBadRequest, "credential must be a Kubernetes credential owned by this workspace")
			return
		}
	}
	if err := s.Store.SetWorkspaceClusterCredential(r.Context(), input.WorkspaceID, r.PathValue("clusterID"), input.CredentialID); err != nil {
		writeStoreError(w, "could not save workspace cluster credential")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_cluster_credential.updated", "cluster", r.PathValue("clusterID"), map[string]string{"workspaceId": input.WorkspaceID, "credentialId": valueOf(input.CredentialID)})
	writeJSON(w, http.StatusOK, map[string]any{"credentialId": input.CredentialID})
}

func (s *Server) listNamespaceBindings(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	if !s.requireWorkspaceCluster(w, r, workspaceID, r.PathValue("clusterID")) {
		return
	}
	items, err := s.Store.ListNamespaceBindings(r.Context(), workspaceID, r.PathValue("clusterID"))
	if err != nil {
		writeStoreError(w, "could not load namespace bindings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listGitSources(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListGitSources(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load Git sources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createGitSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID   string  `json:"workspaceId"`
		Name          string  `json:"name"`
		RepositoryURL string  `json:"repositoryUrl"`
		CredentialID  *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
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
		if err != nil || c.WorkspaceID == nil || *c.WorkspaceID != input.WorkspaceID || (c.Kind != "git-ssh" && c.Kind != "git-https") {
			writeError(w, http.StatusBadRequest, "new Git sources must use a Git credential owned by this workspace")
			return
		}
	}
	source := store.GitSource{ID: store.NewID(), WorkspaceID: input.WorkspaceID, Name: input.Name, RepositoryURL: input.RepositoryURL, CredentialID: input.CredentialID}
	if err := s.Store.CreateGitSource(r.Context(), source); err != nil {
		writeError(w, http.StatusConflict, "could not create Git source")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "git_source.created", "git_source", source.ID, map[string]string{"workspaceId": source.WorkspaceID, "name": source.Name})
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
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListApplications(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load applications")
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
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) getApplicationKustomization(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	if app.Renderer != "kustomize" {
		writeError(w, http.StatusBadRequest, "application does not use Kustomize")
		return
	}
	source, err := s.Store.GitSourceForWorkspace(r.Context(), app.SourceID, app.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "application Git source is unavailable")
		return
	}
	checkout, err := gitops.Fetch(r.Context(), s.Store, s.EncryptionKey, source, app.Revision)
	if err != nil {
		writeClassifiedError(w, http.StatusBadGateway, "Git source could not be fetched", apiErrorMetadata{
			code:           "git.fetch_failed",
			category:       "git",
			retryable:      true,
			remediation:    "Check the repository URL, selected revision, credentials, and Git provider availability.",
			remediationURL: "/settings/connections",
		}, map[string]any{"applicationId": app.ID})
		return
	}
	defer checkout.Close()
	manifestPaths := make([]string, 0, len(app.Namespaces))
	seenPaths := map[string]bool{}
	if len(app.Namespaces) == 0 {
		manifestPaths = append(manifestPaths, app.ManifestPath)
	} else {
		for _, binding := range app.Namespaces {
			manifestPath := app.ManifestPath
			if targetPath := strings.TrimSpace(app.TargetManifestPath); targetPath != "" {
				manifestPath = targetPath
			}
			if namespacePath := strings.TrimSpace(app.NamespaceManifestPaths[binding.Namespace]); namespacePath != "" {
				manifestPath = namespacePath
			}
			if !seenPaths[manifestPath] {
				seenPaths[manifestPath] = true
				manifestPaths = append(manifestPaths, manifestPath)
			}
		}
	}
	gitNamespaces := make([]string, 0, len(manifestPaths))
	seenNamespaces := map[string]bool{}
	for _, manifestPath := range manifestPaths {
		namespace, err := render.KustomizationNamespace(checkout.Root, manifestPath)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if namespace != "" && !seenNamespaces[namespace] {
			seenNamespaces[namespace] = true
			gitNamespaces = append(gitNamespaces, namespace)
		}
	}
	sort.Strings(gitNamespaces)
	writeJSON(w, http.StatusOK, map[string]string{"namespace": strings.Join(gitNamespaces, ", "), "commit": checkout.Commit})
}

func (s *Server) updateApplicationRenderSettings(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if app.BranchTest != nil {
		writeError(w, http.StatusConflict, "finish the branch test before editing render settings")
		return
	}
	if app.RepositoryConfigurationID != "" {
		writeError(w, http.StatusConflict, "application configuration is managed by Git; edit "+app.ConfigurationPath+" in the repository")
		return
	}

	if app.Renderer != "kustomize" {
		writeError(w, http.StatusBadRequest, "render settings are only available for Kustomize applications")
		return
	}
	if app.ApplicationGroupID != "" {
		writeError(w, http.StatusConflict, "Kustomize render settings are managed by the deployment group")
		return
	}
	var input struct {
		KustomizeHelmEnabled       bool `json:"kustomizeHelmEnabled"`
		KustomizeNamespaceOverride bool `json:"kustomizeNamespaceOverride"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.KustomizeNamespaceOverride && len(app.Namespaces) == 0 {
		writeError(w, http.StatusBadRequest, "namespace transform requires at least one bound namespace")
		return
	}
	if err := s.Store.SetApplicationRenderSettings(r.Context(), app.ID, input.KustomizeHelmEnabled, input.KustomizeNamespaceOverride); err != nil {
		writeStoreError(w, "could not update render settings")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.render_settings.updated", "application", app.ID, map[string]bool{"kustomizeHelmEnabled": input.KustomizeHelmEnabled, "kustomizeNamespaceOverride": input.KustomizeNamespaceOverride})
	app.KustomizeHelmEnabled = input.KustomizeHelmEnabled
	app.KustomizeNamespaceOverride = input.KustomizeNamespaceOverride
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID                string                             `json:"workspaceId"`
		Name                       string                             `json:"name"`
		SourceID                   string                             `json:"sourceId"`
		Revision                   string                             `json:"revision"`
		ManifestPath               string                             `json:"manifestPath"`
		TargetManifestPath         string                             `json:"targetManifestPath"`
		NamespaceManifestPaths     map[string]string                  `json:"namespaceManifestPaths"`
		Renderer                   string                             `json:"renderer"`
		KustomizeHelmEnabled       bool                               `json:"kustomizeHelmEnabled"`
		CreateNamespaces           bool                               `json:"createNamespaces"`
		KustomizeNamespaceOverride bool                               `json:"kustomizeNamespaceOverride"`
		HelmValuesFiles            []string                           `json:"helmValuesFiles"`
		HelmValuesYAML             string                             `json:"helmValuesYaml"`
		TargetHelmValuesFiles      []string                           `json:"targetHelmValuesFiles"`
		TargetHelmValuesYAML       string                             `json:"targetHelmValuesYaml"`
		NamespaceHelmValues        map[string]core.HelmValuesOverride `json:"namespaceHelmValues"`
		ClusterID                  string                             `json:"clusterId"`
		Namespaces                 []string                           `json:"namespaces"`
		SyncPolicy                 string                             `json:"syncPolicy"`
		PollSeconds                int                                `json:"pollSeconds"`
		RetryPolicy                *store.RetryPolicy                 `json:"retryPolicy"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
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
	if input.KustomizeHelmEnabled && input.Renderer != "kustomize" {
		writeError(w, http.StatusBadRequest, "Kustomize Helm can only be enabled for Kustomize applications")
		return
	}
	if input.KustomizeNamespaceOverride && input.Renderer != "kustomize" {
		writeError(w, http.StatusBadRequest, "Kustomize namespace transforms require the Kustomize renderer")
		return
	}
	valuesFiles, valuesYAML, err := validateHelmValuesInput(input.Renderer, input.HelmValuesFiles, input.HelmValuesYAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.HelmValuesFiles, input.HelmValuesYAML = valuesFiles, valuesYAML
	targetFiles, targetValues, err := validateHelmValuesInput(input.Renderer, input.TargetHelmValuesFiles, input.TargetHelmValuesYAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.TargetHelmValuesFiles, input.TargetHelmValuesYAML = targetFiles, targetValues
	input.NamespaceHelmValues, err = validateNamespaceHelmValuesInput(input.Renderer, input.NamespaceHelmValues, input.Namespaces)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.TargetManifestPath, input.NamespaceManifestPaths, err = validateKustomizeManifestPaths(input.Renderer, input.TargetManifestPath, input.NamespaceManifestPaths, input.Namespaces)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	retryPolicy := store.DefaultRetryPolicy()
	if input.RetryPolicy != nil {
		retryPolicy = *input.RetryPolicy
	}
	if err := retryPolicy.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceGitSource(w, r, input.WorkspaceID, input.SourceID) {
		return
	}
	if !s.requireWorkspaceCluster(w, r, input.WorkspaceID, input.ClusterID) {
		return
	}
	if len(input.Namespaces) == 0 {
		writeError(w, http.StatusBadRequest, "at least one namespace binding is required")
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
		binding, err := s.Store.NamespaceBinding(r.Context(), input.WorkspaceID, input.ClusterID, namespace)
		if err != nil {
			writeError(w, http.StatusBadRequest, "every application namespace must have a workspace binding")
			return
		}
		bindings = append(bindings, binding)
	}
	app := store.Application{ID: store.NewID(), WorkspaceID: input.WorkspaceID, Name: input.Name, SourceID: input.SourceID, Revision: input.Revision, ManifestPath: input.ManifestPath, TargetManifestPath: input.TargetManifestPath, NamespaceManifestPaths: input.NamespaceManifestPaths, Renderer: input.Renderer, KustomizeHelmEnabled: input.KustomizeHelmEnabled, KustomizeNamespaceOverride: input.KustomizeNamespaceOverride, CreateNamespaces: input.CreateNamespaces, HelmValuesFiles: input.HelmValuesFiles, HelmValuesYAML: input.HelmValuesYAML, TargetHelmValuesFiles: input.TargetHelmValuesFiles, TargetHelmValuesYAML: input.TargetHelmValuesYAML, NamespaceHelmValues: input.NamespaceHelmValues, ClusterID: input.ClusterID, Namespaces: bindings, SyncPolicy: input.SyncPolicy, PollSeconds: input.PollSeconds, RetryPolicy: retryPolicy, Health: "unknown"}
	if err := s.Store.CreateApplication(r.Context(), app); err != nil {
		writeError(w, http.StatusConflict, "could not create application")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.created", "application", app.ID, applicationAuditDetails(app))
	writeJSON(w, http.StatusCreated, app)
}

func applicationAuditDetails(app store.Application) map[string]any {
	namespaceValues := make(map[string]any, len(app.NamespaceHelmValues))
	for namespace, values := range app.NamespaceHelmValues {
		namespaceValues[namespace] = map[string]any{"files": values.Files, "configured": values.YAML != ""}
	}
	return map[string]any{
		"name": app.Name, "workspaceId": app.WorkspaceID, "sourceId": app.SourceID,
		"revision": app.Revision, "manifestPath": app.ManifestPath,
		"targetManifestPath": app.TargetManifestPath, "namespaceManifestPaths": app.NamespaceManifestPaths,
		"renderer": app.Renderer, "clusterId": app.ClusterID, "namespaces": app.Namespaces,
		"syncPolicy": app.SyncPolicy, "pollSeconds": app.PollSeconds,
		"retryPolicy":      app.RetryPolicy,
		"createNamespaces": app.CreateNamespaces, "kustomizeHelmEnabled": app.KustomizeHelmEnabled, "kustomizeNamespaceOverride": app.KustomizeNamespaceOverride,
		"helmValuesFiles": app.HelmValuesFiles, "helmValuesConfigured": app.HelmValuesYAML != "",
		"targetHelmValuesFiles": app.TargetHelmValuesFiles, "targetHelmValuesConfigured": app.TargetHelmValuesYAML != "",
		"namespaceHelmValues": namespaceValues,
	}
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
