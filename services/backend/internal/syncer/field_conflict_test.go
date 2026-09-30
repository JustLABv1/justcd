package syncer

import (
	"errors"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
	"testing"
)

func TestWrappedFieldOwnershipConflictIsReportedForReview(t *testing.T) {
	conflict := apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "ntfy", errors.New(`conflict with "JustCD": .spec.replicas`))
	err := fmt.Errorf("server-side apply failed: %w", conflict)
	classification := ClassifyRetryError(err)
	if classification.Retryable || classification.ErrorCode != "kubernetes.conflict" || classification.TerminalReason != "conflict_requires_review" {
		t.Fatalf("unexpected classification: %+v", classification)
	}
	message := safeApplyFailure(err, store.OperationProgress{Total: 1})
	if !strings.Contains(message, "field ownership conflicts") {
		t.Fatalf("missing actionable conflict explanation: %s", message)
	}
}
