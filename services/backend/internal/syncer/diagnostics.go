package syncer

import (
	"errors"
	"fmt"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// Resource context is separate from the upstream error text, which may contain
// credentials or admission payloads and must not be persisted for display.
type resourceAccessError struct {
	identity core.Identity
	action   string
	err      error
}

func (e *resourceAccessError) Error() string {
	return fmt.Sprintf("%s failed for %s %s/%s: %v", e.action, e.identity.Kind, e.identity.Namespace, e.identity.Name, e.err)
}
func (e *resourceAccessError) Unwrap() error { return e.err }

func PlanFailureDiagnostic(err error) store.ApplicationStatusIssue {
	stage := "check"
	var staged interface{ ErrorStage() string }
	if errors.As(err, &staged) {
		stage = staged.ErrorStage()
	}
	return statusIssue(stage, err, time.Now().UTC())
}
