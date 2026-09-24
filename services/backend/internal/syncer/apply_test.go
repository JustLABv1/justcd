package syncer

import (
	"errors"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestPartialSyncFailureExplainsNoAutomaticRollback(t *testing.T) {
	progress := store.OperationProgress{Total: 4, Completed: []core.Identity{{Kind: "Deployment", Namespace: "apps", Name: "api"}}}
	message := safeApplyFailure(errors.New("update target changed after plan review"), progress)
	for _, fragment := range []string{"1 of 4", "not rolled back", "rebuild the plan"} {
		if !strings.Contains(message, fragment) {
			t.Errorf("partial failure message %q does not contain %q", message, fragment)
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
