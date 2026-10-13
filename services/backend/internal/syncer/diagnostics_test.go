package syncer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestPermissionDiagnosticPreservesResourceContext(t *testing.T) {
	identity := core.Identity{ClusterID: "cluster", APIVersion: "secrets.hashicorp.com/v1beta1", Kind: "VaultStaticSecret", Namespace: "dev", Name: "database"}
	upstream := apierrors.NewForbidden(schema.GroupResource{Group: "secrets.hashicorp.com", Resource: "vaultstaticsecrets"}, identity.Name, fmt.Errorf("bearer do-not-expose"))
	for _, action := range []string{"get", "patch (dry-run)"} {
		err := &planStageError{stage: "cluster", err: fmt.Errorf("wrapped: %w", &resourceAccessError{identity: identity, action: action, err: upstream})}
		issue := PlanFailureDiagnostic(err)
		if issue.Code != "kubernetes.permission_denied" || issue.Resource == nil || *issue.Resource != identity || issue.Action != action {
			t.Fatalf("permission context lost: %+v", issue)
		}
		for _, expected := range []string{action, identity.Kind, identity.APIVersion, identity.Namespace, identity.Name} {
			if !strings.Contains(issue.Summary, expected) {
				t.Fatalf("summary lacks %q: %s", expected, issue.Summary)
			}
		}
		if !strings.Contains(issue.Remediation, "exclude") || strings.Contains(issue.Summary, "do-not-expose") {
			t.Fatalf("unsafe or unhelpful diagnostic: %+v", issue)
		}
	}
}

func TestDiagnosticsDistinguishCredentialsFromConnectivity(t *testing.T) {
	issue := PlanFailureDiagnostic(&planStageError{stage: "cluster", err: apierrors.NewUnauthorized("private details")})
	if issue.Code != "kubernetes.authentication_failed" || strings.Contains(issue.Summary, "private details") {
		t.Fatalf("unexpected authentication diagnostic: %+v", issue)
	}
	issue = PlanFailureDiagnostic(&planStageError{stage: "cluster", err: fmt.Errorf("connection refused")})
	if issue.Code != "" || !strings.Contains(issue.Summary, "reached") {
		t.Fatalf("connectivity misclassified as RBAC: %+v", issue)
	}
}

func TestClusterScopeRequirementHasSpecificDiagnostic(t *testing.T) {
	identity := core.Identity{ClusterID: "cluster", APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: "mimir-rollout-operator-webhook-clusterrole", ClusterScoped: true}
	err := fmt.Errorf("internal wrapper: %w", clusterScopeRequired(&identity))
	issue := PlanFailureDiagnostic(err)
	if issue.Code != "kubernetes.cluster_scope_required" || issue.Source != "cluster" || issue.Resource == nil || *issue.Resource != identity || !strings.Contains(issue.Summary, identity.Name) || !strings.Contains(issue.Remediation, "PR-discovered") {
		t.Fatalf("cluster-scope context missing: %+v", issue)
	}
	if strings.Contains(issue.Summary, "internal wrapper") {
		t.Fatal("wrapper leaked into user-facing cause")
	}
}
