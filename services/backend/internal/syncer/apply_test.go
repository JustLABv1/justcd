package syncer

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGroupVersionResolvesCoreAndGroupedResources(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}, {Group: "apps", Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	for _, identity := range []core.Identity{
		{APIVersion: "v1", Kind: "ConfigMap"},
		{APIVersion: "apps/v1", Kind: "Deployment"},
	} {
		if _, err := mapper.RESTMapping(groupKind(identity), groupVersion(identity)); err != nil {
			t.Errorf("cannot map %s %s: %v", identity.APIVersion, identity.Kind, err)
		}
	}
}

func TestPartialSyncFailureExplainsNoAutomaticRollback(t *testing.T) {
	progress := store.OperationProgress{Total: 4, Completed: []core.Identity{{Kind: "Deployment", Namespace: "apps", Name: "api"}}}
	message := safeApplyFailure(errors.New("update target changed after plan review"), progress, "sync")
	for _, fragment := range []string{"1 of 4", "not rolled back", "Live state or ownership changed after review", "rebuild the plan"} {
		if !strings.Contains(message, fragment) {
			t.Errorf("partial failure message %q does not contain %q", message, fragment)
		}
	}
}

func TestPartialRollbackFailureIsNotReportedAsSync(t *testing.T) {
	progress := store.OperationProgress{Total: 3, Completed: []core.Identity{{Kind: "Deployment", Namespace: "apps", Name: "api"}}}
	message := safeApplyFailure(errors.New("update target changed after plan review"), progress, "rollback")
	for _, fragment := range []string{"Rollback stopped", "1 of 3 resources", "were not rolled back"} {
		if !strings.Contains(message, fragment) {
			t.Errorf("rollback failure message %q does not contain %q", message, fragment)
		}
	}
}

func TestResourceVersionChangedBetweenPreflightAndStepStopsSync(t *testing.T) {
	identity := core.Identity{Kind: "Deployment", Namespace: "apps", Name: "api"}
	err := validateUpdatePreconditions(identity, "uid-1", "17", "app-a", "uid-1", "18", "app-a")
	if err == nil || !strings.Contains(err.Error(), "changed after plan review") {
		t.Fatalf("expected stale resource-version precondition to stop the step, got %v", err)
	}
}

func TestCleanupFailureReportsAlreadyAppliedResource(t *testing.T) {
	for _, completed := range []int{0, 1} {
		progress := store.OperationProgress{Total: 2, Completed: make([]core.Identity, completed)}
		cause := fmt.Errorf("resource: %w", &AppliedResourceError{Cause: errors.New("target changed after apply")})
		message := safeApplyFailure(cause, progress)
		for _, fragment := range []string{"Kubernetes applied the current resource", "ownership cleanup failed", "No automatic rollback", fmt.Sprintf("%d of 2", completed)} {
			if !strings.Contains(message, fragment) {
				t.Fatalf("missing %q in %q", fragment, message)
			}
		}
	}
}
