package syncer

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestKubernetesFailureCausesKeepFieldPathsWithoutRejectedValues(t *testing.T) {
	path := `.spec.template.spec.containers[name="alloy"].securityContext.fsGroup`
	err := &planStageError{stage: "cluster", err: &resourceAccessError{
		identity: core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "loki-prd", Name: "alloy"},
		action:   "patch (dry-run)",
		err:      fmt.Errorf("failed to create typed patch object (loki-prd/alloy; apps/v1, Kind=Deployment): %s: field not declared in schema", path),
	}}
	issue := PlanFailureDiagnostic(err)
	if len(issue.Causes) != 1 || issue.Causes[0].Field != path || issue.Causes[0].Reason != "FieldNotDeclared" || !strings.Contains(issue.Summary, path) {
		t.Fatalf("schema failure detail lost: %+v", issue)
	}

	upstream := &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonInvalid, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{
		{Type: metav1.CauseTypeFieldValueInvalid, Field: "spec.selector", Message: `Invalid value: private-value: field is immutable`},
		{Type: metav1.CauseTypeFieldValueRequired, Field: "spec.template.spec.containers[0].image", Message: "private-value"},
		{Type: metav1.CauseTypeFieldValueInvalid, Field: `data["private-value"]`, Message: "private-value"},
		{Type: "private-value", Field: "spec.replicas", Message: "private-value"},
	}}}}
	issue = PlanFailureDiagnostic(&planStageError{stage: "cluster", err: fmt.Errorf("wrapped: %w", upstream)})
	encoded, _ := json.Marshal(issue)
	if len(issue.Causes) != 4 || issue.Causes[0].Reason != "FieldValueImmutable" || issue.Causes[1].Field != "spec.template.spec.containers[0].image" || issue.Causes[2].Field != "" || strings.Contains(string(encoded), "private-value") {
		t.Fatalf("unsafe or incomplete causes: %s", encoded)
	}
}

func TestKubernetesFailureCausesAreBoundedAndRejectUnsafePaths(t *testing.T) {
	upstream := &apierrors.StatusError{ErrStatus: metav1.Status{Details: &metav1.StatusDetails{}}}
	for i := 0; i < 30; i++ {
		upstream.ErrStatus.Details.Causes = append(upstream.ErrStatus.Details.Causes, metav1.StatusCause{Type: metav1.CauseTypeFieldValueRequired, Field: fmt.Sprintf("spec.field%d", i)})
	}
	if causes := kubernetesFailureCauses(upstream); len(causes) != 20 {
		t.Fatalf("causes not bounded: %d", len(causes))
	}
	for _, path := range []string{`data["secret-value"]`, ".spec.foo\nprivate-value", strings.Repeat("x", 1025), `.spec.containers[name="bearer private-value"].image`} {
		if safeDiagnosticField(path) != "" {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	causes := kubernetesFailureCauses(fmt.Errorf(`strict decoding error: unknown field "spec.foo", unknown field "spec.foo"`))
	if len(causes) != 1 || causes[0].Field != "spec.foo" {
		t.Fatalf("unknown field parsing failed: %+v", causes)
	}
}

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
	if issue.Code != "kubernetes.connection_failed" || !strings.Contains(issue.Summary, "connection failed") {
		t.Fatalf("connectivity misclassified as RBAC: %+v", issue)
	}
}

func TestPlanCheckFailuresDoNotImplyConnectivityFailure(t *testing.T) {
	identity := core.Identity{APIVersion: "apps/v1", Kind: "StatefulSet", Namespace: "loki-prd", Name: "loki"}
	for _, test := range []struct {
		err  error
		code string
	}{
		{apierrors.NewBadRequest("private manifest values"), "kubernetes.validation_failed"},
		{fmt.Errorf(`failed to create typed patch object: .spec.template.spec.containers[name="alloy"].securityContext.fsGroup: field not declared in schema`), "kubernetes.validation_failed"},
		{apierrors.NewConflict(schema.GroupResource{Resource: "statefulsets"}, "loki", fmt.Errorf("private values")), "kubernetes.conflict"},
		{fmt.Errorf("private agent-named-resource validation failure"), "kubernetes.plan_check_failed"},
		{apierrors.NewTimeoutError("private values", 1), "kubernetes.request_timeout"},
		{apierrors.NewInternalError(fmt.Errorf("private admission response")), "kubernetes.request_failed"},
	} {
		err := &planStageError{stage: "cluster", err: &resourceAccessError{identity: identity, action: "patch (dry-run)", err: test.err}}
		issue := PlanFailureDiagnostic(err)
		if issue.Code != test.code || issue.Resource == nil || *issue.Resource != identity || issue.Action != "patch (dry-run)" || !strings.Contains(issue.Summary, "loki") {
			t.Fatalf("plan failure lost context: %+v", issue)
		}
		if strings.Contains(issue.Summary, "private") || strings.Contains(issue.Summary, "connectivity") {
			t.Fatalf("unsafe or misleading diagnostic: %+v", issue)
		}
	}
	issue := PlanFailureDiagnostic(&planStageError{stage: "cluster", err: fmt.Errorf("wrapped: %w", &kube.AgentError{Code: "cluster.agent_unavailable", Message: "private response"})})
	if issue.Code != "cluster.agent_unavailable" || strings.Contains(issue.Summary, "private") {
		t.Fatalf("agent diagnostic lost or exposed upstream response: %+v", issue)
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
