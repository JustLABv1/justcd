package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

type permissionTestDiscovery struct {
	discovery.DiscoveryInterface
	versionErr error
	groupsErr  error
}

func (d permissionTestDiscovery) ServerVersion() (*version.Info, error) {
	if d.versionErr != nil {
		return nil, d.versionErr
	}
	return &version.Info{GitVersion: "v1.31.0"}, nil
}

func (d permissionTestDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, d.groupsErr
}

func permissionTestClients(t *testing.T, allowed func(namespace, apiGroup, resource, verb string) bool) *Clients {
	t.Helper()
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetResource().Group != "authorization.k8s.io" {
			return true, nil, fmt.Errorf("unexpected access-review group %q", action.GetResource().Group)
		}
		create, ok := action.(ktesting.CreateAction)
		if !ok {
			return true, nil, errors.New("access review was not a create action")
		}
		object, ok := create.GetObject().(*unstructured.Unstructured)
		if !ok {
			return true, nil, errors.New("access review was not unstructured")
		}
		attributes, _, err := unstructured.NestedMap(object.Object, "spec", "resourceAttributes")
		if err != nil {
			return true, nil, err
		}
		stringValue := func(key string) string {
			value, _ := attributes[key].(string)
			return value
		}
		result := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "authorization.k8s.io/v1",
			"kind":       "SelfSubjectAccessReview",
			"status":     map[string]any{"allowed": allowed(stringValue("namespace"), stringValue("apiGroup"), stringValue("resource"), stringValue("verb"))},
		}}
		return true, result, nil
	})
	return &Clients{
		Dynamic:   dynamicClient,
		Discovery: permissionTestDiscovery{},
	}
}

func TestCheckPermissionsReportsPartialAccessAndOnlyCreatesReviews(t *testing.T) {
	client := permissionTestClients(t, func(_, apiGroup, resource, verb string) bool {
		return apiGroup == "" && resource == "pods" && (verb == "get" || verb == "list")
	})
	report := CheckPermissions(context.Background(), client, "payments", nil)
	if report.Status != "partial" {
		t.Fatalf("status = %q, want partial", report.Status)
	}
	if !report.CanReadPods {
		t.Fatal("CanReadPods = false, want true when both Pod read checks pass")
	}
	if report.ServerVersion != "v1.31.0" {
		t.Fatalf("server version = %q", report.ServerVersion)
	}
	if !strings.Contains(report.Suggestions.RoleYAML, "kind: Role") || !strings.Contains(report.Suggestions.RoleYAML, "create") {
		t.Fatalf("missing namespace Role suggestion for denied permissions: %s", report.Suggestions.RoleYAML)
	}
	if strings.Contains(report.Suggestions.RoleYAML, "pods") {
		t.Fatalf("suggested Role should only contain missing permissions, got: %s", report.Suggestions.RoleYAML)
	}
	if len(client.Dynamic.(*fake.FakeDynamicClient).Actions()) != len(namespacePermissionSpecs) {
		t.Fatalf("got %d dynamic actions, want one review per namespace permission", len(client.Dynamic.(*fake.FakeDynamicClient).Actions()))
	}
	for _, action := range client.Dynamic.(*fake.FakeDynamicClient).Actions() {
		if action.GetResource().Resource != "selfsubjectaccessreviews" {
			t.Fatalf("self-test performed unexpected Kubernetes action against %s", action.GetResource().Resource)
		}
	}
}

func TestCheckPermissionsSeparatesClusterScopeChecks(t *testing.T) {
	namespaceClient := permissionTestClients(t, func(namespace, _, resource, verb string) bool {
		return namespace == "payments" && resource == "pods" && verb == "get"
	})
	clusterClient := permissionTestClients(t, func(namespace, _, resource, verb string) bool {
		return namespace == "" && resource == "namespaces" && (verb == "get" || verb == "list")
	})
	report := CheckPermissions(context.Background(), namespaceClient, "payments", clusterClient)
	if report.ClusterScope == nil || report.ClusterScope.Status != "partial" {
		t.Fatalf("cluster scope = %#v, want partial", report.ClusterScope)
	}
	if report.Suggestions.ClusterRoleYAML == "" || !strings.Contains(report.Suggestions.ClusterRoleYAML, "kind: ClusterRole") {
		t.Fatalf("missing ClusterRole suggestion: %s", report.Suggestions.ClusterRoleYAML)
	}
	for _, check := range report.ClusterScope.Checks {
		if check.Namespace != "" || check.Scope != "cluster" {
			t.Fatalf("cluster-scope check unexpectedly names a namespace: %#v", check)
		}
	}
	for _, action := range clusterClient.Dynamic.(*fake.FakeDynamicClient).Actions() {
		if action.GetResource().Resource != "selfsubjectaccessreviews" {
			t.Fatalf("cluster self-test performed unexpected action against %s", action.GetResource().Resource)
		}
	}
}

func TestCheckPermissionsMarksReviewFailuresUnknown(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "selfsubjectaccessreviews", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "authorization.k8s.io", Resource: "selfsubjectaccessreviews"}, "", errors.New("not allowed"))
	})
	report := CheckPermissions(context.Background(), &Clients{Dynamic: client, Discovery: permissionTestDiscovery{}}, "payments", nil)
	if report.Status != "failed" || report.Failure == nil || report.Failure.Code != "kubernetes.permission_review_forbidden" {
		t.Fatalf("unexpected report after denied review request: %#v", report)
	}
	if report.Checks[1].Status != "unknown" || report.Checks[len(report.Checks)-1].Status != "unknown" {
		t.Fatalf("checks after review failure should be unknown: %#v", report.Checks)
	}
}

func TestClassifyPermissionTestError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "rejected credential", err: apierrors.NewUnauthorized("invalid token: super-secret"), code: "kubernetes.credential_rejected"},
		{name: "forbidden discovery", err: apierrors.NewForbidden(schema.GroupResource{}, "", errors.New("forbidden")), code: "kubernetes.discovery_forbidden"},
		{name: "TLS", err: errors.New("x509: certificate signed by unknown authority"), code: "kubernetes.tls_failed"},
		{name: "expired credential", err: errors.New("Kubernetes credential has expired"), code: "kubernetes.credential_expired"},
		{name: "unusable credential", err: errors.New("no Kubernetes credential is configured for this binding"), code: "kubernetes.credential_unusable"},
		{name: "unreachable API", err: errors.New("dial tcp: connection refused"), code: "kubernetes.connection_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := ClassifyPermissionTestError(test.err)
			if failure.Code != test.code {
				t.Fatalf("code = %q, want %q", failure.Code, test.code)
			}
			if strings.Contains(failure.Message, "super-secret") {
				t.Fatalf("failure message leaked credential material: %q", failure.Message)
			}
		})
	}
}
