// Package agentprotocol defines the bounded Kubernetes request protocol. It never
// carries Kubernetes credentials or an arbitrary upstream URL.
package agentprotocol

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

const Version = 1
const MaxBody = 8 << 20

type Profile struct {
	Name             string   `json:"name"`
	WorkspaceIDs     []string `json:"workspaceIds"`
	Namespaces       []string `json:"namespaces"`
	ClusterScope     bool     `json:"clusterScope"`
	TokenFile        string   `json:"tokenFile,omitempty"`
	DeniedNamespaces []string `json:"deniedNamespaces,omitempty"`
}
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
	Execution    *Execution `json:"execution,omitempty"`
	ID           string     `json:"id"`
	Lease        string     `json:"lease"`
	WorkspaceID  string     `json:"workspaceId"`
	Profile      string     `json:"profile"`
	Namespace    string     `json:"namespace"`
	ClusterScope bool       `json:"clusterScope"`
	Method       string     `json:"method"`
	URI          string     `json:"uri"`
	ContentType  string     `json:"contentType"`
	Accept       string     `json:"accept"`
	Body         []byte     `json:"body,omitempty"`
	Deadline     time.Time  `json:"deadline"`
}
type Result struct {
	ErrorCode   string `json:"errorCode,omitempty"`
	Lease       string `json:"lease"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        []byte `json:"body,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Validate checks both routing and the local operator's authority. Discovery is
// available to profiles, but cluster resources require explicit authorization.
func Validate(r Request, p Profile) error {
	if r.Profile != p.Name || !contains(p.WorkspaceIDs, r.WorkspaceID) {
		return errors.New("workspace is not authorized by the local agent profile")
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
		case "secrets", "configmaps", "pods", "services", "serviceaccounts", "persistentvolumeclaims", "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "cronjobs", "roles", "rolebindings", "ingresses", "networkpolicies":
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
	} else if namespace == "" && (!r.ClusterScope || !p.ClusterScope) {
		return errors.New("cluster-scoped resources require a local cluster-scope profile")
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
		if x == s || (strings.HasSuffix(x, "-*") && strings.HasPrefix(s, strings.TrimSuffix(x, "*"))) {
			return true
		}
	}
	return false
}
