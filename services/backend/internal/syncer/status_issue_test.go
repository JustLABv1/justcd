package syncer

import (
	"errors"
	"github.com/justlab/justcd/services/backend/internal/core"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
	"testing"
	"time"
)

func TestOwnershipConflictStatusDoesNotReportConnectivityFailure(t *testing.T) {
	conflict := &OwnershipConflict{Identity: core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "existing", Name: "web"}}
	for _, err := range []error{conflict, &OwnershipConflicts{Items: []*OwnershipConflict{conflict}}, &planStageError{stage: "cluster", err: &OwnershipConflicts{Items: []*OwnershipConflict{conflict}}}} {
		at := time.Now().UTC()
		issue := statusIssue("cluster", err, at)
		if issue.Code != "kubernetes.ownership_conflict" || issue.Action != "adopt" || issue.Resource == nil || *issue.Resource != conflict.Identity || !issue.ObservedAt.Equal(at) {
			t.Fatalf("incorrect adoption issue: %+v", issue)
		}
		if strings.Contains(issue.Summary, "could not be reached") || issue.Remediation == "" {
			t.Fatalf("misleading adoption issue: %+v", issue)
		}
	}
}

func TestClusterStatusRetainsConnectivityAndPermissionFailures(t *testing.T) {
	issue := statusIssue("cluster", errors.New("connection refused"), time.Now())
	if !strings.Contains(issue.Summary, "could not be reached") {
		t.Fatalf("connectivity issue changed: %+v", issue)
	}
	issue = statusIssue("cluster", apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "web", errors.New("denied")), time.Now())
	if issue.Code != "kubernetes.permission_denied" {
		t.Fatalf("permission issue changed: %+v", issue)
	}
}
