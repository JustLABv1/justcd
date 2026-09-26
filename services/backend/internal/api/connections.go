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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (s *Server) updateCredential(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.CredentialByID(r.Context(), r.PathValue("credentialID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}
	if c.ProjectID != nil {
		if !s.requireProjectRole(w, r, *c.ProjectID, "owner") {
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
	if !s.requireProjectRole(w, r, source.ProjectID, "owner") {
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
	source.Name, source.RepositoryURL, source.CredentialID = strings.TrimSpace(input.Name), strings.TrimSpace(input.RepositoryURL), input.CredentialID
	if source.Name == "" || len(source.Name) > 100 || !validGitURL(source.RepositoryURL) {
		writeError(w, http.StatusBadRequest, "name and a supported SSH or HTTPS Git URL are required")
		return
	}
	if source.CredentialID != nil {
		c, err := s.Store.CredentialByID(r.Context(), *source.CredentialID)
		if err != nil || (c.ProjectID != nil && *c.ProjectID != source.ProjectID) || (c.Kind != "git-ssh" && c.Kind != "git-https") {
			writeError(w, http.StatusBadRequest, "credential must be a Git credential available to this project")
			return
		}
	}
	if err := s.Store.UpdateGitSource(r.Context(), source); err != nil {
		writeError(w, http.StatusConflict, "could not update Git source")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "git_source.updated", "git_source", source.ID, map[string]string{"projectId": source.ProjectID, "name": source.Name})
	writeJSON(w, http.StatusOK, source)
}

func (s *Server) updateNamespaceBinding(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID    string  `json:"projectId"`
		CredentialID *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireProjectRole(w, r, input.ProjectID, "owner") {
		return
	}
	clusterID, namespace := r.PathValue("clusterID"), r.PathValue("namespace")
	if _, err := s.Store.NamespaceBinding(r.Context(), input.ProjectID, clusterID, namespace); err != nil {
		writeError(w, http.StatusNotFound, "namespace binding not found")
		return
	}
	cluster, err := s.Store.ClusterByID(r.Context(), clusterID)
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
	} else {
		projectID, err := s.Store.ProjectClusterCredential(r.Context(), input.ProjectID, clusterID)
		if err != nil {
			writeStoreError(w, "could not load project cluster credential")
			return
		}
		if projectID == nil && cluster.DefaultCredentialID == nil {
			writeError(w, http.StatusBadRequest, "a namespace or cluster default credential is required")
			return
		}
	}
	if err := s.Store.UpdateNamespaceBinding(r.Context(), input.ProjectID, clusterID, namespace, input.CredentialID); err != nil {
		writeStoreError(w, "could not update namespace binding")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "namespace_binding.updated", "cluster", clusterID, map[string]string{"projectId": input.ProjectID, "namespace": namespace, "credentialId": valueOf(input.CredentialID)})
	writeJSON(w, http.StatusOK, map[string]any{"namespace": namespace, "credentialId": input.CredentialID})
}

func (s *Server) testGitSource(w http.ResponseWriter, r *http.Request) {
	source, err := s.Store.GitSourceByID(r.Context(), r.PathValue("sourceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	if !s.requireProjectRole(w, r, source.ProjectID, "viewer") {
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
	var input struct {
		Name                     string  `json:"name"`
		APIServer                string  `json:"apiServer"`
		CADataBase64             *string `json:"caDataBase64"`
		InsecureSkipVerify       bool    `json:"insecureSkipVerify"`
		DefaultCredentialID      *string `json:"defaultCredentialId"`
		ClusterScopeCredentialID *string `json:"clusterScopeCredentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
		if err != nil || (c.Kind != "kubernetes-token" && c.Kind != "kubeconfig") || c.ProjectID != nil {
			writeError(w, http.StatusBadRequest, "cluster credentials must be global Kubernetes credentials")
			return
		}
	}
	cluster.APIServer, cluster.InsecureSkipVerify = endpoint.String(), input.InsecureSkipVerify
	cluster.DefaultCredentialID, cluster.ClusterScopeCredential = input.DefaultCredentialID, input.ClusterScopeCredentialID
	if err := s.Store.UpdateCluster(r.Context(), cluster); err != nil {
		writeError(w, http.StatusConflict, "could not update cluster")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "cluster.updated", "cluster", cluster.ID, map[string]string{"name": cluster.Name, "apiServer": cluster.APIServer})
	cluster.CAData = nil
	writeJSON(w, http.StatusOK, cluster)
}

func (s *Server) testCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID string `json:"projectId"`
		Namespace string `json:"namespace"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireProjectRole(w, r, input.ProjectID, "viewer") {
		return
	}
	cluster, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	credentialID, err := s.Store.ProjectClusterCredential(r.Context(), input.ProjectID, cluster.ID)
	if err != nil {
		writeStoreError(w, "could not load project cluster credential")
		return
	}
	if input.Namespace != "" {
		binding, err := s.Store.NamespaceBinding(r.Context(), input.ProjectID, cluster.ID, input.Namespace)
		if err != nil {
			writeError(w, http.StatusBadRequest, "namespace is not bound to this project")
			return
		}
		if binding.CredentialID != nil {
			credentialID = binding.CredentialID
		}
	} else if credentialID == nil && cluster.DefaultCredentialID == nil {
		bindings, err := s.Store.ListNamespaceBindings(r.Context(), input.ProjectID, cluster.ID)
		if err != nil {
			writeStoreError(w, "could not load namespace bindings")
			return
		}
		for _, binding := range bindings {
			if binding.CredentialID != nil {
				credentialID = binding.CredentialID
				input.Namespace = binding.Namespace
				break
			}
		}
	}
	client, err := kube.ForBinding(r.Context(), s.Store, s.EncryptionKey, cluster, credentialID, false)
	if err != nil {
		writeClassifiedError(w, http.StatusUnprocessableEntity, "Cluster credential could not be used", apiErrorMetadata{
			code:           "kubernetes.credential_unusable",
			category:       "kubernetes",
			retryable:      false,
			remediation:    "Check the credential type, expiration, cluster selection, and namespace binding.",
			remediationURL: "/settings/connections",
		}, nil)
		return
	}
	version, err := client.Discovery.ServerVersion()
	if err != nil {
		writeClassifiedError(w, http.StatusUnprocessableEntity, "Kubernetes API connection failed", apiErrorMetadata{
			code:           "kubernetes.connection_failed",
			category:       "kubernetes",
			retryable:      true,
			remediation:    "Check the API endpoint, TLS trust configuration, network path, and credential permissions.",
			remediationURL: "/settings/connections",
		}, nil)
		return
	}
	namespace := input.Namespace
	if namespace == "" {
		namespace = "default"
	}
	review := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview", "spec": map[string]any{"resourceAttributes": map[string]any{"namespace": namespace, "verb": "get", "resource": "pods"}}}}
	result, err := client.Dynamic.Resource(schema.GroupVersionResource{Group: "authorization.k8s.io", Version: "v1", Resource: "selfsubjectaccessreviews"}).Create(r.Context(), review, metav1.CreateOptions{})
	if err != nil {
		writeClassifiedError(w, http.StatusUnprocessableEntity, "Kubernetes authentication check failed", apiErrorMetadata{
			code:           "kubernetes.permission_check_failed",
			category:       "kubernetes",
			retryable:      false,
			remediation:    "Allow the credential to create SelfSubjectAccessReview requests, then run the connection test again.",
			remediationURL: "/settings/connections",
		}, nil)
		return
	}
	allowed, _, _ := unstructured.NestedBool(result.Object, "status", "allowed")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "serverVersion": version.GitVersion, "namespace": namespace, "canReadPods": allowed})
}
