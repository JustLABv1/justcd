package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) updateCredential(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.CredentialByID(r.Context(), r.PathValue("credentialID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}
	if c.WorkspaceID != nil {
		if !s.requireWorkspaceRole(w, r, *c.WorkspaceID, "owner") {
			return
		}
	} else if !currentUser(r).IsAdmin {
		writeError(w, http.StatusForbidden, "instance administrator role required")
		return
	}
	var input struct {
		Name      string            `json:"name"`
		ExpiresAt *string           `json:"expiresAt"`
		Secret    map[string]string `json:"secret"`
		Username  *string           `json:"username"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c.Name = strings.TrimSpace(input.Name)
	if c.Name == "" || len(c.Name) > 100 {
		writeError(w, http.StatusBadRequest, "credential name is required and must be at most 100 characters")
		return
	}
	if input.Username != nil && c.Kind != "git-https" {
		writeError(w, http.StatusBadRequest, "username is only supported for Git HTTPS credentials")
		return
	}
	if input.Username != nil {
		value := strings.TrimSpace(*input.Username)
		if value == "" {
			writeError(w, http.StatusBadRequest, "Git username is required")
			return
		}
		if input.Secret == nil {
			plain, err := security.Decrypt(s.EncryptionKey, c.Cipher, "credential:"+c.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "could not read credential")
				return
			}
			if err := json.Unmarshal(plain, &input.Secret); err != nil {
				writeError(w, http.StatusInternalServerError, "credential payload is invalid")
				return
			}
		}
		input.Secret["username"] = value
	}
	if input.Secret != nil {
		if err := validateCredentialPayload(c.Kind, input.Secret); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		payload, _ := json.Marshal(input.Secret)
		c.Cipher, err = security.Encrypt(s.EncryptionKey, payload, "credential:"+c.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not encrypt credential")
			return
		}
	}
	if c.Kind == "git-https" {
		c.Username, err = s.gitHTTPSUsername(c)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read Git credential username")
			return
		}
	}
	if input.ExpiresAt != nil {
		c.ExpiresAt = nil
	}
	if input.ExpiresAt != nil && *input.ExpiresAt != "" {
		expires, err := timeParse(*input.ExpiresAt)
		if err != nil || !expires.After(time.Now()) {
			writeError(w, http.StatusBadRequest, "expiresAt must be a future RFC3339 timestamp")
			return
		}
		c.ExpiresAt = &expires
	}
	if err := s.Store.UpdateCredential(r.Context(), c); err != nil {
		writeStoreError(w, "could not update credential")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "credential.updated", "credential", c.ID, map[string]any{"name": c.Name, "secretRotated": input.Secret != nil})
	c.Cipher = nil
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) gitHTTPSUsername(c store.Credential) (string, error) {
	plain, err := security.Decrypt(s.EncryptionKey, c.Cipher, "credential:"+c.ID)
	if err != nil {
		return "", err
	}
	var secret struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(plain, &secret); err != nil {
		return "", err
	}
	if strings.TrimSpace(secret.Username) == "" {
		return "justcd", nil
	}
	return secret.Username, nil
}

func (s *Server) updateGitSource(w http.ResponseWriter, r *http.Request) {
	source, err := s.Store.GitSourceByID(r.Context(), r.PathValue("sourceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, source.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Name          string  `json:"name"`
		RepositoryURL string  `json:"repositoryUrl"`
		CredentialID  *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	previousCredentialID := source.CredentialID
	source.Name, source.RepositoryURL, source.CredentialID = strings.TrimSpace(input.Name), strings.TrimSpace(input.RepositoryURL), input.CredentialID
	if source.Name == "" || len(source.Name) > 100 || !validGitURL(source.RepositoryURL) {
		writeError(w, http.StatusBadRequest, "name and a supported SSH or HTTPS Git URL are required")
		return
	}
	if source.CredentialID != nil {
		c, err := s.Store.CredentialByID(r.Context(), *source.CredentialID)
		legacyReferenceUnchanged := previousCredentialID != nil && *previousCredentialID == *source.CredentialID && c.WorkspaceID == nil
		if err != nil || (!legacyReferenceUnchanged && (c.WorkspaceID == nil || *c.WorkspaceID != source.WorkspaceID)) || (c.Kind != "git-ssh" && c.Kind != "git-https") {
			writeError(w, http.StatusBadRequest, "new Git source credentials must be owned by this workspace")
			return
		}
	}
	if err := s.Store.UpdateGitSource(r.Context(), source); err != nil {
		writeError(w, http.StatusConflict, "could not update Git source")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "git_source.updated", "git_source", source.ID, map[string]string{"workspaceId": source.WorkspaceID, "name": source.Name})
	writeJSON(w, http.StatusOK, source)
}

func (s *Server) updateNamespaceBinding(w http.ResponseWriter, r *http.Request) {
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
	clusterID, namespace := r.PathValue("clusterID"), r.PathValue("namespace")
	if _, err := s.Store.NamespaceBinding(r.Context(), input.WorkspaceID, clusterID, namespace); err != nil {
		writeError(w, http.StatusNotFound, "namespace binding not found")
		return
	}
	cluster, err := s.Store.ClusterByID(r.Context(), clusterID)
	if err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	if !s.requireWorkspaceCluster(w, r, input.WorkspaceID, clusterID) {
		return
	}
	if input.CredentialID != nil {
		c, err := s.Store.CredentialByID(r.Context(), *input.CredentialID)
		if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || (c.WorkspaceID != nil && *c.WorkspaceID != input.WorkspaceID) {
			writeError(w, http.StatusBadRequest, "credential must be a Kubernetes credential available to this workspace")
			return
		}
	} else {
		workspaceID, err := s.Store.WorkspaceClusterCredential(r.Context(), input.WorkspaceID, clusterID)
		if err != nil {
			writeStoreError(w, "could not load workspace cluster credential")
			return
		}
		if workspaceID == nil && cluster.DefaultCredentialID == nil {
			writeError(w, http.StatusBadRequest, "a namespace or cluster default credential is required")
			return
		}
	}
	if err := s.Store.UpdateNamespaceBinding(r.Context(), input.WorkspaceID, clusterID, namespace, input.CredentialID); err != nil {
		writeStoreError(w, "could not update namespace binding")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "namespace_binding.updated", "cluster", clusterID, map[string]string{"workspaceId": input.WorkspaceID, "namespace": namespace, "credentialId": valueOf(input.CredentialID)})
	writeJSON(w, http.StatusOK, map[string]any{"namespace": namespace, "credentialId": input.CredentialID})
}

func (s *Server) testGitSource(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") || !s.requireWorkspaceGitSource(w, r, workspaceID, r.PathValue("sourceID")) {
		return
	}
	source, err := s.Store.GitSourceForWorkspace(r.Context(), r.PathValue("sourceID"), workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	checkout, err := gitops.Fetch(r.Context(), s.Store, s.EncryptionKey, source, "HEAD")
	if err != nil {
		writeClassifiedError(w, http.StatusUnprocessableEntity, "Git source connection failed", apiErrorMetadata{
			code:           "git.connection_failed",
			category:       "git",
			retryable:      true,
			remediation:    "Check the repository URL, selected revision, credentials, and Git provider availability.",
			remediationURL: "/settings/connections",
		}, nil)
		return
	}
	defer checkout.Close()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "commit": checkout.Commit})
}

func (s *Server) updateCluster(w http.ResponseWriter, r *http.Request) {
	cluster, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	if cluster.WorkspaceID == nil {
		if !currentUser(r).IsAdmin {
			writeError(w, http.StatusForbidden, "instance administrator role required to edit a legacy cluster")
			return
		}
	} else if !s.requireWorkspaceRole(w, r, *cluster.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Name                     string  `json:"name"`
		APIServer                string  `json:"apiServer"`
		CADataBase64             *string `json:"caDataBase64"`
		InsecureSkipVerify       bool    `json:"insecureSkipVerify"`
		DefaultCredentialID      *string `json:"defaultCredentialId"`
		ClusterScopeCredentialID *string `json:"clusterScopeCredentialId"`
		MaxConcurrentOperations  *int    `json:"maxConcurrentOperations"`
		OperationsPerMinute      *int    `json:"operationsPerMinute"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if cluster.WorkspaceID != nil && (input.DefaultCredentialID != nil || input.ClusterScopeCredentialID != nil) {
		writeError(w, http.StatusBadRequest, "workspace clusters use a private workspace credential instead of instance-wide cluster defaults")
		return
	}
	maxConcurrentOperations, operationsPerMinute := cluster.MaxConcurrentOperations, cluster.OperationsPerMinute
	if input.MaxConcurrentOperations != nil {
		maxConcurrentOperations = *input.MaxConcurrentOperations
	}
	if input.OperationsPerMinute != nil {
		operationsPerMinute = *input.OperationsPerMinute
	}
	if maxConcurrentOperations == 0 {
		maxConcurrentOperations = 2
	}
	if operationsPerMinute == 0 {
		operationsPerMinute = 30
	}
	if !validClusterOperationLimits(maxConcurrentOperations, operationsPerMinute) {
		writeError(w, http.StatusBadRequest, "cluster operation limits must allow 1–20 concurrent operations and 1–1000 operations per minute")
		return
	}
	endpoint, err := url.Parse(strings.TrimSpace(input.APIServer))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		writeError(w, http.StatusBadRequest, "apiServer must be an HTTPS Kubernetes API URL")
		return
	}
	cluster.Name = strings.TrimSpace(input.Name)
	if cluster.Name == "" || len(cluster.Name) > 100 {
		writeError(w, http.StatusBadRequest, "cluster name is required and must be at most 100 characters")
		return
	}
	if input.CADataBase64 != nil {
		cluster.CAData, err = base64.StdEncoding.DecodeString(*input.CADataBase64)
		if err != nil || len(cluster.CAData) > 128<<10 {
			writeError(w, http.StatusBadRequest, "CA data must be valid base64 and at most 128 KiB")
			return
		}
	}
	for _, id := range []*string{input.DefaultCredentialID, input.ClusterScopeCredentialID} {
		if id == nil {
			continue
		}
		c, err := s.Store.CredentialByID(r.Context(), *id)
		if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || c.WorkspaceID != nil {
			writeError(w, http.StatusBadRequest, "cluster credentials must be global Kubernetes credentials")
			return
		}
	}
	cluster.APIServer, cluster.InsecureSkipVerify = endpoint.String(), input.InsecureSkipVerify
	cluster.DefaultCredentialID, cluster.ClusterScopeCredential = input.DefaultCredentialID, input.ClusterScopeCredentialID
	cluster.MaxConcurrentOperations, cluster.OperationsPerMinute = maxConcurrentOperations, operationsPerMinute
	if err := s.Store.UpdateCluster(r.Context(), cluster); err != nil {
		writeError(w, http.StatusConflict, "could not update cluster")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "cluster.updated", "cluster", cluster.ID, map[string]string{"name": cluster.Name, "apiServer": cluster.APIServer})
	cluster.CAData = nil
	writeJSON(w, http.StatusOK, cluster)
}

func validClusterOperationLimits(maxConcurrent, operationsPerMinute int) bool {
	if maxConcurrent == 0 {
		maxConcurrent = 2
	}
	if operationsPerMinute == 0 {
		operationsPerMinute = 30
	}
	return maxConcurrent >= 1 && maxConcurrent <= 20 && operationsPerMinute >= 1 && operationsPerMinute <= 1000
}

func (s *Server) testCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID         string `json:"workspaceId"`
		Namespace           string `json:"namespace"`
		IncludeClusterScope bool   `json:"includeClusterScope"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	minimumRole := "viewer"
	if input.IncludeClusterScope {
		minimumRole = "owner"
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, minimumRole) {
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
	credentialID, err := s.Store.WorkspaceClusterCredential(r.Context(), input.WorkspaceID, cluster.ID)
	if err != nil {
		writeStoreError(w, "could not load workspace cluster credential")
		return
	}
	if input.Namespace != "" {
		binding, err := s.Store.NamespaceBinding(r.Context(), input.WorkspaceID, cluster.ID, input.Namespace)
		if err != nil {
			writeError(w, http.StatusBadRequest, "namespace is not bound to this workspace")
			return
		}
		if binding.CredentialID != nil {
			credentialID = binding.CredentialID
		}
	} else {
		bindings, err := s.Store.ListNamespaceBindings(r.Context(), input.WorkspaceID, cluster.ID)
		if err != nil {
			writeStoreError(w, "could not load namespace bindings")
			return
		}
		if len(bindings) > 0 {
			input.Namespace = bindings[0].Namespace
			if bindings[0].CredentialID != nil {
				credentialID = bindings[0].CredentialID
			}
		}
	}
	namespace := input.Namespace
	if namespace == "" {
		namespace = "default"
	}
	client, clientErr := kube.ForBinding(r.Context(), s.Store, s.EncryptionKey, cluster, credentialID, false)
	var clusterClient *kube.Clients
	var clusterScopeFailure *kube.PermissionFailure
	clusterScopeStatus := ""
	if input.IncludeClusterScope {
		if cluster.ClusterScopeCredential == nil {
			// Access reviews are read-only. Testing the effective namespace
			// credential does not authorize cluster-scoped deployments.
			clusterClient = client
			if clientErr != nil {
				failure := kube.ClassifyPermissionTestError(clientErr)
				clusterScopeFailure = &failure
			}
		} else {
			clusterClient, err = kube.ForBinding(r.Context(), s.Store, s.EncryptionKey, cluster, cluster.ClusterScopeCredential, true)
			if err != nil {
				failure := kube.ClassifyPermissionTestError(err)
				clusterScopeFailure = &failure
			}
		}
	}
	var report kube.PermissionReport
	if clientErr != nil {
		report = kube.FailedPermissionReport(namespace, clientErr)
		if clusterClient != nil {
			clusterScope := kube.CheckClusterScopePermissions(r.Context(), clusterClient)
			report.ClusterScope = &clusterScope
		}
	} else {
		report = kube.CheckPermissions(r.Context(), client, namespace, clusterClient)
	}
	if input.IncludeClusterScope && report.ClusterScope == nil {
		switch {
		case clusterScopeStatus == "not_configured":
			report.ClusterScope = &kube.ClusterScopePermissions{Status: "not_configured", Checks: []kube.PermissionCheck{}}
		case clusterScopeFailure != nil:
			report.ClusterScope = &kube.ClusterScopePermissions{Status: "failed", Checks: []kube.PermissionCheck{}, Failure: clusterScopeFailure}
		default:
			report.ClusterScope = &kube.ClusterScopePermissions{Status: "not_run", Checks: []kube.PermissionCheck{}}
		}
	}
	report.CheckedAt = time.Now().UTC()
	reportJSON, err := json.Marshal(report)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode Kubernetes permission report")
		return
	}
	if err := s.Store.SaveKubernetesPermissionTest(r.Context(), store.KubernetesPermissionTest{
		WorkspaceID: input.WorkspaceID, ClusterID: cluster.ID, Namespace: namespace,
		Report: reportJSON, CheckedAt: report.CheckedAt,
	}); err != nil {
		writeStoreError(w, "could not save Kubernetes permission report")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "kubernetes.permission_tested", "cluster", cluster.ID, map[string]any{"workspaceId": input.WorkspaceID, "namespace": namespace, "includeClusterScope": input.IncludeClusterScope, "status": report.Status})
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) listClusterPermissionTests(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	clusterID := r.PathValue("clusterID")
	if !s.requireWorkspaceCluster(w, r, workspaceID, clusterID) {
		return
	}
	if _, err := s.Store.ClusterByID(r.Context(), clusterID); err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	items, err := s.Store.ListKubernetesPermissionTests(r.Context(), workspaceID, clusterID)
	if err != nil {
		writeStoreError(w, "could not load Kubernetes permission reports")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
