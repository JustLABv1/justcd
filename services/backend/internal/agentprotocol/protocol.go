// Package agentprotocol defines the bounded Kubernetes request protocol. It never
// carries Kubernetes credentials or an arbitrary upstream URL.
package agentprotocol

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

const Version = 1
const MaxBody = 8 << 20

type Profile struct {
	Name                  string                 `json:"name"`
	WorkspaceIDs          []string               `json:"workspaceIds"`
	Namespaces            []string               `json:"namespaces"`
	ClusterScope          bool                   `json:"clusterScope"`
	TokenFile             string                 `json:"tokenFile,omitempty"`
	DeniedNamespaces      []string               `json:"deniedNamespaces,omitempty"`
	DeploymentCredentials *DeploymentCredentials `json:"deploymentCredentials,omitempty"`
}

// DeploymentCredentials is operator-owned policy. Callers cannot choose an
// account, audience, or lifetime. Provisioning is a separate opt-in local policy.
type DeploymentCredentials struct {
	LifetimeSeconds int64                  `json:"lifetimeSeconds"`
	Audience        string                 `json:"audience,omitempty"`
	Provisioning    *NamespaceProvisioning `json:"provisioning,omitempty"`
}

// NamespaceProvisioning is local operator policy, never repository input.
type NamespaceProvisioning struct {
	Platform              string `json:"platform,omitempty"`
	DeploymentClusterRole string `json:"deploymentClusterRole"`
	DeleteNamespaces      bool   `json:"deleteNamespaces"`
}
type EnvironmentRequest struct {
	ID                string `json:"id"`
	ApplicationID     string `json:"applicationId"`
	Action            string `json:"action"`
	CreateNamespace   bool   `json:"createNamespace"`
	PreserveNamespace bool   `json:"preserveNamespace"`
	Shared            bool   `json:"shared"`
	ExpectedUID       string `json:"expectedUid,omitempty"`
}
type EnvironmentStatus struct {
	NamespaceUID string   `json:"namespaceUid"`
	Phase        string   `json:"phase"`
	Managed      bool     `json:"managed"`
	Shared       bool     `json:"shared"`
	FailedPhase  string   `json:"failedPhase,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Resources    []string `json:"resources,omitempty"`
}

const DeploymentServiceAccount = "justcd-deployer"

type Identity struct {
	ClusterUID string    `json:"clusterUid"`
	Token      string    `json:"token"`
	ClusterID  string    `json:"clusterId"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type Enrollment struct {
	ClusterUID string    `json:"clusterUid"`
	Protocol   int       `json:"protocol"`
	Version    string    `json:"version"`
	Profiles   []Profile `json:"profiles"`
}
type Execution struct {
	OperationID string `json:"operationId"`
	PlanID      string `json:"planId"`
	Digest      string `json:"digest"`
}

type Request struct {
	Environment  *EnvironmentRequest `json:"environment,omitempty"`
	Execution    *Execution          `json:"execution,omitempty"`
	ID           string              `json:"id"`
	Lease        string              `json:"lease"`
	WorkspaceID  string              `json:"workspaceId"`
	Profile      string              `json:"profile"`
	Namespace    string              `json:"namespace"`
	ClusterScope bool                `json:"clusterScope"`
	Method       string              `json:"method"`
	URI          string              `json:"uri"`
	ContentType  string              `json:"contentType"`
	Accept       string              `json:"accept"`
	Body         []byte              `json:"body,omitempty"`
	Deadline     time.Time           `json:"deadline"`
}
type Result struct {
	Environment *EnvironmentStatus `json:"environment,omitempty"`
	ErrorCode   string             `json:"errorCode,omitempty"`
	Lease       string             `json:"lease"`
	Status      int                `json:"status"`
	ContentType string             `json:"contentType"`
	Body        []byte             `json:"body,omitempty"`
	Error       string             `json:"error,omitempty"`
}

// Validate checks both routing and the local operator's authority. Discovery is
// available to profiles, but cluster resources require explicit authorization.
func Validate(r Request, p Profile) error {
	if r.Profile != p.Name || !contains(p.WorkspaceIDs, r.WorkspaceID) {
		return errors.New("workspace is not authorized by the local agent profile")
	}
	if p.DeploymentCredentials != nil {
		if err := ValidateDeploymentProfile(p); err != nil {
			return err
		}
		if !validNamespace(r.Namespace) || r.ClusterScope || !namespaceAllowed(p.Namespaces, r.Namespace) || ProtectedNamespace(r.Namespace) || namespaceAllowed(p.DeniedNamespaces, r.Namespace) {
			return errors.New("managed deployment credentials require an authorized, unprotected namespace and namespace scope")
		}
	}
	if r.Environment != nil {
		if p.DeploymentCredentials == nil || p.DeploymentCredentials.Provisioning == nil || r.ClusterScope || !r.Deadline.After(time.Now()) || r.Method != "POST" || r.URI != "/justcd/environments" || len(r.Body) != 0 {
			return errors.New("environment lifecycle requires a locally authorized provisioning profile")
		}
		e := r.Environment
		validID := func(s string) bool {
			if len(s) == 0 || len(s) > 128 {
				return false
			}
			for _, c := range s {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
					return false
				}
			}
			return true
		}
		if !validID(e.ID) || !validID(e.ApplicationID) || (e.Action != "ensure" && e.Action != "release") || !validNamespace(r.Namespace) {
			return errors.New("invalid environment lifecycle request")
		}
		return nil
	}
	if r.ClusterScope && !p.ClusterScope {
		return errors.New("local agent profile does not authorize cluster scope")
	}
	if !r.ClusterScope && r.Namespace != "" && !namespaceAllowed(p.Namespaces, r.Namespace) {
		return errors.New("namespace is not authorized by the local agent profile")
	}
	if !r.Deadline.After(time.Now()) {
		return errors.New("task expired before execution")
	}
	if len(r.Body) > MaxBody {
		return errors.New("request exceeds agent limit")
	}
	switch r.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return errors.New("unsupported Kubernetes method")
	}
	u, err := url.ParseRequestURI(r.URI)
	if err != nil || strings.HasPrefix(r.URI, "//") || u.IsAbs() || u.Host != "" || strings.Contains(u.Path, "//") || strings.Contains(u.Path, "..") || u.Path != u.EscapedPath() {
		return errors.New("invalid Kubernetes request path")
	}
	q := u.Query()
	for k := range q {
		switch k {
		case "timeout", "timeoutSeconds", "dryRun", "fieldManager", "fieldValidation", "force", "labelSelector", "fieldSelector", "limit", "continue", "resourceVersion", "resourceVersionMatch", "pretty":
		default:
			return errors.New("unsupported Kubernetes query parameter")
		}
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	if r.Method == "GET" && (path == "version" || path == "api" || path == "apis" || (len(parts) == 2 && parts[0] == "api") || (len(parts) <= 3 && parts[0] == "apis") || strings.HasPrefix(path, "openapi/")) {
		return nil
	}
	if path == "apis/authorization.k8s.io/v1/selfsubjectaccessreviews" && r.Method == "POST" {
		return nil
	}
	offset := 0
	if len(parts) >= 2 && parts[0] == "api" {
		offset = 2
	} else if len(parts) >= 3 && parts[0] == "apis" {
		offset = 3
	} else {
		return errors.New("only Kubernetes API resources are allowed")
	}
	tail := parts[offset:]
	if p.DeploymentCredentials != nil {
		// Discovery and access reviews returned above still use the deployment
		// identity. Every resource operation must target the bound namespace.
		if len(tail) < 3 || tail[0] != "namespaces" || tail[1] != r.Namespace {
			return errors.New("managed deployment credentials cannot access cluster resources")
		}
		if (parts[0] == "apis" && (parts[1] == "rbac.authorization.k8s.io" || parts[1] == "authentication.k8s.io" || parts[1] == "authorization.k8s.io")) || tail[2] == "serviceaccounts" {
			return errors.New("managed deployment credentials cannot manage authentication or authorization")
		}
	}
	// OpenShift project bootstrap uses the workload identity so requester access
	// is granted to that account. Bind these cluster API calls to the routed
	// namespace rather than requiring a different, broader workload identity.
	if parts[0] == "apis" && len(parts) >= 3 && parts[1] == "project.openshift.io" && parts[2] == "v1" {
		project := ""
		if len(tail) == 2 && tail[0] == "projects" && r.Method == "GET" {
			project = tail[1]
		} else if len(tail) == 1 && tail[0] == "projectrequests" && r.Method == "POST" {
			var object struct {
				APIVersion string `json:"apiVersion"`
				Kind       string `json:"kind"`
				Metadata   struct {
					Name string `json:"name"`
				} `json:"metadata"`
			}
			if json.Unmarshal(r.Body, &object) != nil || object.APIVersion != "project.openshift.io/v1" || object.Kind != "ProjectRequest" || !validNamespace(object.Metadata.Name) {
				return errors.New("invalid OpenShift project request")
			}
			project = object.Metadata.Name
			if !p.ClusterScope {
				return errors.New("OpenShift project creation requires a local cluster-scope profile")
			}
		}
		if project != "" {
			if !validNamespace(project) || ProtectedNamespace(project) || namespaceAllowed(p.DeniedNamespaces, project) {
				return errors.New("OpenShift project is protected by the local agent profile")
			}
			// A promoted bootstrap task still belongs to its bound namespace.
			// Recheck local policy even when the backend marked the API call as
			// cluster-scoped; the enrolled backend policy may be stale.
			if (r.Namespace != "" || !r.ClusterScope) && (r.Namespace != project || !namespaceAllowed(p.Namespaces, project)) {
				return errors.New("OpenShift project does not match the authorized namespace")
			}
			return nil
		}
	}
	namespace := ""
	if len(tail) >= 3 && tail[0] == "namespaces" {
		namespace = tail[1]
		tail = tail[2:]
	} else if len(tail) == 2 && tail[0] == "namespaces" {
		namespace = tail[1]
		if r.Method != "GET" && !(r.ClusterScope && p.ClusterScope) {
			return errors.New("namespace mutations require a local cluster-scope profile")
		}
	}
	// A cluster-wide collection can otherwise disclose resources from protected
	// namespaces (especially the enrollment/profile credential Secrets).
	if namespace == "" && len(tail) > 0 && len(p.DeniedNamespaces) > 0 {
		switch tail[0] {
		case "secrets", "configmaps", "pods", "services", "serviceaccounts", "persistentvolumeclaims", "deployments", "statefulsets", "controllerrevisions", "daemonsets", "replicasets", "jobs", "cronjobs", "roles", "rolebindings", "ingresses", "networkpolicies":
			return errors.New("cross-namespace collections are disabled when protected namespaces are configured")
		}
	}
	if namespace != "" && namespaceAllowed(p.DeniedNamespaces, namespace) {
		return errors.New("namespace is protected by the local agent profile")
	}
	if len(tail) < 1 || len(tail) > 2 {
		return errors.New("Kubernetes subresources are not supported by the agent")
	}
	if namespace != "" && !(r.ClusterScope && p.ClusterScope) {
		if r.Namespace != namespace || !namespaceAllowed(p.Namespaces, namespace) {
			return errors.New("namespace is not authorized by the local agent profile")
		}
	} else if namespace == "" {
		if !r.ClusterScope {
			return errors.New("cluster-scoped resource was sent through a namespace-scoped route")
		}
		if !p.ClusterScope {
			return errors.New("cluster-scoped resources require a local cluster-scope profile")
		}
	}
	return nil
}

func ProtectedNamespace(namespace string) bool {
	return namespace == "default" || namespace == "kube-system" || namespace == "kube-public" || namespace == "kube-node-lease"
}

func ValidateDeploymentProfile(p Profile) error {
	if p.DeploymentCredentials == nil {
		return nil
	}
	c := p.DeploymentCredentials
	if p.ClusterScope || len(p.WorkspaceIDs) != 1 || len(p.Namespaces) == 0 || c.LifetimeSeconds < 600 || c.LifetimeSeconds > 3600 || len(c.Audience) > 2048 {
		return errors.New("managed credentials require one workspace, authorized namespaces, namespace scope, and a lifetime of 600–3600 seconds")
	}
	if c.Provisioning != nil {
		if platform := c.Provisioning.Platform; platform != "" && platform != "kubernetes" && platform != "openshift" {
			return errors.New("provisioning platform must be kubernetes or openshift")
		}
		role := c.Provisioning.DeploymentClusterRole
		if role == "" || len(role) > 253 || strings.ContainsAny(role, "/%\\") {
			return errors.New("provisioning requires a locally approved deployment ClusterRole")
		}
	}
	for _, ns := range p.Namespaces {
		if len(ns) == 0 || len(ns) > 63 || ProtectedNamespace(ns) || namespaceAllowed(p.DeniedNamespaces, ns) {
			return errors.New("managed credentials target an invalid or protected namespace")
		}
		target := ns
		if c.Provisioning != nil && strings.HasSuffix(ns, "-*") {
			target = strings.TrimSuffix(ns, "*") + "x"
		}
		if !validNamespace(target) {
			return errors.New("managed credentials require DNS namespace names or provisioning prefixes ending in -*")
		}

	}
	return nil
}

// ValidateDeploymentProfiles prevents a second local profile from exposing a
// managed namespace to another workspace or a broader static identity.
func ValidateDeploymentProfiles(profiles []Profile) error {
	for i, p := range profiles {
		if err := ValidateDeploymentProfile(p); err != nil {
			return err
		}
		if p.DeploymentCredentials == nil {
			continue
		}
		for j, other := range profiles {
			if i == j {
				continue
			}
			for _, namespace := range p.Namespaces {
				if other.ClusterScope || namespaceAllowed(other.Namespaces, namespace) || patternsOverlap(p.Namespaces, other.Namespaces) {
					return errors.New("managed namespaces cannot overlap another local profile or a cluster-scope profile")
				}
			}
		}
	}
	return nil
}
func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func namespaceAllowed(v []string, s string) bool {
	for _, x := range v {
		if x == "*" || x == s || (strings.HasSuffix(x, "-*") && strings.HasPrefix(s, strings.TrimSuffix(x, "*"))) {
			return true
		}
	}
	return false
}

func validNamespace(ns string) bool {
	if len(ns) == 0 || len(ns) > 63 {
		return false
	}
	for i, ch := range ns {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || (ch == '-' && i > 0 && i < len(ns)-1)) {
			return false
		}
	}
	return true
}
func patternsOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if namespaceAllowed([]string{x}, y) || namespaceAllowed([]string{y}, x) {
				return true
			}
		}
	}
	return false
}
