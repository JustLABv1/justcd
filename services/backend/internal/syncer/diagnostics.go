package syncer

import (
	"errors"
	"fmt"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// PlanConfigurationError carries application configuration context suitable for
// display. Unlike upstream errors, it contains no provider responses or manifests.
type PlanConfigurationError struct {
	Source      string
	Code        string
	Summary     string
	Remediation string
	Resource    *core.Identity
}

func (e *PlanConfigurationError) Error() string      { return e.Summary }
func (e *PlanConfigurationError) ErrorStage() string { return e.Source }

func clusterScopeRequired(identity *core.Identity) error {
	summary := "Cluster-scoped access requires a privileged cluster-scope credential."
	if identity != nil {
		summary = fmt.Sprintf("Cluster-scoped resource %s/%s requires a privileged cluster-scope credential.", identity.Kind, identity.Name)
	}
	return &PlanConfigurationError{Source: "cluster", Code: "kubernetes.cluster_scope_required", Summary: summary,
		Remediation: "Configure a cluster-scope credential for trusted applications that manage cluster-wide resources, or remove/exclude those resources from this application's manifests. PR-discovered applications only permit namespaced manifests; adding a credential does not lift that restriction.", Resource: identity}
}

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
