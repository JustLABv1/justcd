package kube

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

type PermissionCheck struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Scope     string `json:"scope"`
	Namespace string `json:"namespace,omitempty"`
	APIGroup  string `json:"apiGroup"`
	Resource  string `json:"resource"`
	Verb      string `json:"verb"`
	Status    string `json:"status"`
	Allowed   bool   `json:"allowed"`
}

type PermissionFailure struct {
	Code        string `json:"code"`
	Category    string `json:"category"`
	Message     string `json:"message"`
	Remediation string `json:"remediation"`
	Retryable   bool   `json:"retryable"`
}

type RBACSuggestions struct {
	RoleYAML               string `json:"roleYaml,omitempty"`
	RoleBindingYAML        string `json:"roleBindingYaml,omitempty"`
	ClusterRoleYAML        string `json:"clusterRoleYaml,omitempty"`
	ClusterRoleBindingYAML string `json:"clusterRoleBindingYaml,omitempty"`
}

type ClusterScopePermissions struct {
	Status  string             `json:"status"`
	Checks  []PermissionCheck  `json:"checks"`
	Failure *PermissionFailure `json:"failure,omitempty"`
}

type PermissionReport struct {
	Status        string                   `json:"status"`
	Namespace     string                   `json:"namespace"`
	ServerVersion string                   `json:"serverVersion,omitempty"`
	CanReadPods   bool                     `json:"canReadPods"`
	CheckedAt     time.Time                `json:"checkedAt,omitempty"`
	Checks        []PermissionCheck        `json:"checks"`
	ClusterScope  *ClusterScopePermissions `json:"clusterScope,omitempty"`
	Suggestions   RBACSuggestions          `json:"suggestions"`
	Failure       *PermissionFailure       `json:"failure,omitempty"`
}

// FailedPermissionReport returns a redacted report when a client cannot be built
// or the Kubernetes API cannot be reached before permission checks begin.
func FailedPermissionReport(namespace string, err error) PermissionReport {
	failure := ClassifyPermissionTestError(err)
	return PermissionReport{
		Status:    "failed",
		Namespace: namespace,
		Checks:    []PermissionCheck{{ID: "api-discovery", Title: "Kubernetes API discovery", Scope: "cluster", Status: "failed"}},
		Failure:   &failure,
	}
}

type permissionSpec struct {
	id       string
	title    string
	apiGroup string
	resource string
	verb     string
}

var namespacePermissionSpecs = []permissionSpec{
	{id: "get-pods", title: "Read Pods", resource: "pods", verb: "get"},
	{id: "list-pods", title: "List Pods", resource: "pods", verb: "list"},
	{id: "get-deployments", title: "Read Deployments", apiGroup: "apps", resource: "deployments", verb: "get"},
	{id: "list-deployments", title: "List Deployments", apiGroup: "apps", resource: "deployments", verb: "list"},
	{id: "list-statefulsets", title: "List StatefulSets", apiGroup: "apps", resource: "statefulsets", verb: "list"},
	{id: "list-daemonsets", title: "List DaemonSets", apiGroup: "apps", resource: "daemonsets", verb: "list"},
	{id: "list-replicasets", title: "List ReplicaSets", apiGroup: "apps", resource: "replicasets", verb: "list"},
	{id: "list-jobs", title: "List Jobs", apiGroup: "batch", resource: "jobs", verb: "list"},
	{id: "create-deployments", title: "Create Deployments", apiGroup: "apps", resource: "deployments", verb: "create"},
	{id: "apply-deployments", title: "Server-side apply Deployments", apiGroup: "apps", resource: "deployments", verb: "patch"},
	{id: "delete-deployments", title: "Delete Deployments", apiGroup: "apps", resource: "deployments", verb: "delete"},
}

var clusterScopePermissionSpecs = []permissionSpec{
	{id: "get-namespaces", title: "Read Namespaces", resource: "namespaces", verb: "get"},
	{id: "list-namespaces", title: "List Namespaces", resource: "namespaces", verb: "list"},
	{id: "create-namespaces", title: "Create Namespaces", resource: "namespaces", verb: "create"},
	{id: "apply-namespaces", title: "Server-side apply Namespaces", resource: "namespaces", verb: "patch"},
	{id: "delete-namespaces", title: "Delete Namespaces", resource: "namespaces", verb: "delete"},
}

// CheckPermissions performs only discovery and SelfSubjectAccessReview calls.
// It never creates, patches, or deletes workload resources.
func CheckPermissions(ctx context.Context, namespaceClient *Clients, namespace string, clusterClient *Clients) PermissionReport {
	report := PermissionReport{
		Status:    "failed",
		Namespace: namespace,
		Checks:    []PermissionCheck{},
	}

	version, err := namespaceClient.Discovery.ServerVersion()
	if err != nil {
		failure := ClassifyPermissionTestError(err)
		report.Failure = &failure
		report.Checks = append(report.Checks, PermissionCheck{ID: "api-discovery", Title: "Kubernetes API discovery", Scope: "cluster", Status: "failed"})
		return report
	}
	report.ServerVersion = version.GitVersion
	if _, _, err := namespaceClient.Discovery.ServerGroupsAndResources(); err != nil {
		failure := ClassifyPermissionTestError(err)
		report.Failure = &failure
		report.Checks = append(report.Checks, PermissionCheck{ID: "api-discovery", Title: "Kubernetes API discovery", Scope: "cluster", Status: "failed"})
		return report
	}
	report.Checks = append(report.Checks, PermissionCheck{ID: "api-discovery", Title: "Kubernetes API discovery", Scope: "cluster", Status: "passed", Allowed: true})

	unknownFailure := runPermissionChecks(ctx, namespaceClient, namespace, "namespace", namespacePermissionSpecs, &report.Checks)
	if unknownFailure != nil {
		report.Failure = unknownFailure
	}
	report.CanReadPods = checkAllowed(report.Checks, "get-pods") && checkAllowed(report.Checks, "list-pods")

	if clusterClient != nil {
		clusterResult := CheckClusterScopePermissions(ctx, clusterClient)
		report.ClusterScope = &clusterResult
	}

	report.Status = summarizeChecks(report.Checks, report.Failure != nil)
	report.Suggestions = permissionSuggestions(namespace, report.Checks, report.ClusterScope)
	return report
}

func CheckClusterScopePermissions(ctx context.Context, client *Clients) ClusterScopePermissions {
	result := ClusterScopePermissions{Status: "failed", Checks: []PermissionCheck{}}
	unknownFailure := runPermissionChecks(ctx, client, "", "cluster", clusterScopePermissionSpecs, &result.Checks)
	if unknownFailure != nil {
		result.Failure = unknownFailure
	}
	result.Status = summarizeChecks(result.Checks, unknownFailure != nil)
	return result
}

func runPermissionChecks(ctx context.Context, client *Clients, namespace, scope string, specs []permissionSpec, destination *[]PermissionCheck) *PermissionFailure {
	for index, spec := range specs {
		check := PermissionCheck{
			ID: spec.id, Title: spec.title, Scope: scope, Namespace: namespace,
			APIGroup: spec.apiGroup, Resource: spec.resource, Verb: spec.verb,
		}
		allowed, err := reviewPermission(ctx, client, namespace, scope, spec)
		if err != nil {
			check.Status = "unknown"
			*destination = append(*destination, check)
			for _, remaining := range specs[index+1:] {
				*destination = append(*destination, PermissionCheck{ID: remaining.id, Title: remaining.title, Scope: scope, Namespace: namespace, APIGroup: remaining.apiGroup, Resource: remaining.resource, Verb: remaining.verb, Status: "unknown"})
			}
			failure := ClassifyPermissionTestError(err)
			if apierrors.IsForbidden(err) {
				failure = PermissionFailure{Code: "kubernetes.permission_review_forbidden", Category: "authorization", Message: "The credential cannot submit Kubernetes permission reviews.", Remediation: "Allow the credential to create SelfSubjectAccessReview requests, then run the self-test again."}
			} else if failure.Code == "kubernetes.connection_failed" {
				failure.Code = "kubernetes.permission_review_unavailable"
				failure.Category = "authorization"
				failure.Message = "Kubernetes permission reviews could not be completed."
				failure.Remediation = "Check whether the credential can submit SelfSubjectAccessReview requests and whether the API server is reachable."
			}
			return &failure
		}
		check.Allowed = allowed
		if allowed {
			check.Status = "passed"
		} else {
			check.Status = "missing"
		}
		*destination = append(*destination, check)
	}
	return nil
}

func reviewPermission(ctx context.Context, client *Clients, namespace, scope string, spec permissionSpec) (bool, error) {
	attributes := map[string]any{
		"verb":     spec.verb,
		"resource": spec.resource,
		"apiGroup": spec.apiGroup,
	}
	if scope == "namespace" {
		attributes["namespace"] = namespace
	}
	review := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "authorization.k8s.io/v1",
		"kind":       "SelfSubjectAccessReview",
		"spec": map[string]any{
			"resourceAttributes": attributes,
		},
	}}
	result, err := client.Dynamic.Resource(schema.GroupVersionResource{Group: "authorization.k8s.io", Version: "v1", Resource: "selfsubjectaccessreviews"}).Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	if evaluationError, _, _ := unstructured.NestedString(result.Object, "status", "evaluationError"); evaluationError != "" {
		return false, errors.New("Kubernetes permission review could not evaluate the requested capability")
	}
	allowed, found, err := unstructured.NestedBool(result.Object, "status", "allowed")
	if err != nil || !found {
		return false, errors.New("Kubernetes permission review returned no decision")
	}
	return allowed, nil
}

func checkAllowed(checks []PermissionCheck, id string) bool {
	for _, check := range checks {
		if check.ID == id {
			return check.Allowed && check.Status == "passed"
		}
	}
	return false
}

func summarizeChecks(checks []PermissionCheck, hasFailure bool) string {
	if hasFailure {
		return "failed"
	}
	for _, check := range checks {
		if check.Status != "passed" {
			return "partial"
		}
	}
	return "passed"
}

func permissionSuggestions(namespace string, checks []PermissionCheck, clusterScope *ClusterScopePermissions) RBACSuggestions {
	var suggestions RBACSuggestions
	if role, binding, ok := rbacYAML("namespace", namespace, checks); ok {
		suggestions.RoleYAML = role
		suggestions.RoleBindingYAML = binding
	}
	if clusterScope != nil {
		if role, binding, ok := rbacYAML("cluster", "", clusterScope.Checks); ok {
			suggestions.ClusterRoleYAML = role
			suggestions.ClusterRoleBindingYAML = binding
		}
	}
	return suggestions
}

func rbacYAML(scope, namespace string, checks []PermissionCheck) (string, string, bool) {
	verbsByResource := map[string]map[string]map[string]bool{}
	for _, check := range checks {
		if check.Scope != scope || check.Resource == "" || check.Status != "missing" {
			continue
		}
		if verbsByResource[check.APIGroup] == nil {
			verbsByResource[check.APIGroup] = map[string]map[string]bool{}
		}
		if verbsByResource[check.APIGroup][check.Resource] == nil {
			verbsByResource[check.APIGroup][check.Resource] = map[string]bool{}
		}
		verbsByResource[check.APIGroup][check.Resource][check.Verb] = true
	}
	if len(verbsByResource) == 0 {
		return "", "", false
	}

	apiGroups := make([]string, 0, len(verbsByResource))
	for group := range verbsByResource {
		apiGroups = append(apiGroups, group)
	}
	slicesSort(apiGroups)
	rules := make([]map[string]any, 0)
	for _, group := range apiGroups {
		resources := make([]string, 0, len(verbsByResource[group]))
		for resource := range verbsByResource[group] {
			resources = append(resources, resource)
		}
		slicesSort(resources)
		for _, resource := range resources {
			verbs := make([]string, 0, len(verbsByResource[group][resource]))
			for verb := range verbsByResource[group][resource] {
				verbs = append(verbs, verb)
			}
			slicesSort(verbs)
			rules = append(rules, map[string]any{"apiGroups": []string{group}, "resources": []string{resource}, "verbs": verbs})
		}
	}

	roleName := "justcd-required-access"
	bindingName := "justcd-required-access"
	roleKind, bindingKind := "Role", "RoleBinding"
	roleMeta := map[string]any{"name": roleName}
	bindingMeta := map[string]any{"name": bindingName}
	if scope == "namespace" {
		roleMeta["namespace"] = namespace
		bindingMeta["namespace"] = namespace
	} else {
		roleKind, bindingKind = "ClusterRole", "ClusterRoleBinding"
	}
	role := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       roleKind,
		"metadata":   roleMeta,
		"rules":      rules,
	}
	binding := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       bindingKind,
		"metadata":   bindingMeta,
		"subjects": []map[string]any{{
			"kind":     "User",
			"apiGroup": "rbac.authorization.k8s.io",
			"name":     "REPLACE_WITH_KUBERNETES_USERNAME",
		}},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     roleKind,
			"name":     roleName,
		},
	}
	roleBytes, roleErr := yaml.Marshal(role)
	bindingBytes, bindingErr := yaml.Marshal(binding)
	if roleErr != nil || bindingErr != nil {
		return "", "", false
	}
	return string(roleBytes), string(bindingBytes), true
}

func slicesSort(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func ClassifyPermissionTestError(err error) PermissionFailure {
	if err == nil {
		return PermissionFailure{}
	}
	if apierrors.IsUnauthorized(err) {
		return PermissionFailure{Code: "kubernetes.credential_rejected", Category: "authentication", Message: "The Kubernetes API rejected the configured credential.", Remediation: "Replace or rotate the Kubernetes credential, then run the self-test again."}
	}
	if apierrors.IsForbidden(err) {
		return PermissionFailure{Code: "kubernetes.discovery_forbidden", Category: "authorization", Message: "The credential cannot access Kubernetes API discovery.", Remediation: "Grant read access to the Kubernetes API discovery endpoints, then rerun the self-test."}
	}
	var unknownAuthority x509.UnknownAuthorityError
	var certificateInvalid x509.CertificateInvalidError
	if errors.As(err, &unknownAuthority) || errors.As(err, &certificateInvalid) || strings.Contains(strings.ToLower(err.Error()), "x509:") {
		return PermissionFailure{Code: "kubernetes.tls_failed", Category: "kubernetes", Message: "TLS verification failed while connecting to the Kubernetes API.", Remediation: "Check the cluster CA certificate and TLS settings. Keep certificate verification enabled outside temporary development clusters."}
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "expired") {
		return PermissionFailure{Code: "kubernetes.credential_expired", Category: "authentication", Message: "The configured Kubernetes credential has expired.", Remediation: "Replace or rotate the expired Kubernetes credential."}
	}
	if strings.Contains(lower, "no kubernetes credential") || strings.Contains(lower, "credential is not a kubernetes") || strings.Contains(lower, "credential payload") || strings.Contains(lower, "kubeconfig") || strings.Contains(lower, "token is empty") || strings.Contains(lower, "decrypt") {
		return PermissionFailure{Code: "kubernetes.credential_unusable", Category: "authentication", Message: "The configured Kubernetes credential cannot be used.", Remediation: "Check that the credential is a supported Kubernetes token or kubeconfig and that its contents are valid."}
	}
	if strings.Contains(lower, "unsupported authentication method") || strings.Contains(lower, "current-context is missing") || strings.Contains(lower, "cluster is missing") || strings.Contains(lower, "user is missing") {
		return PermissionFailure{Code: "kubernetes.credential_unusable", Category: "authentication", Message: "The configured Kubernetes credential cannot be used.", Remediation: "Check that the credential is a supported Kubernetes token or kubeconfig and that its contents are valid."}
	}
	var operationError *net.OpError
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &operationError) || strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "i/o timeout") {
		return PermissionFailure{Code: "kubernetes.connection_failed", Category: "dependency", Message: "JustCD could not reach the Kubernetes API server.", Remediation: "Check the API endpoint, network path, firewall, and cluster availability, then retry.", Retryable: true}
	}
	return PermissionFailure{Code: "kubernetes.connection_failed", Category: "kubernetes", Message: "Kubernetes access could not be verified.", Remediation: "Check the Kubernetes endpoint, credential, and cluster availability."}
}
