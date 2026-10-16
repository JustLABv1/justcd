package syncer

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var (
	// Only field names, numeric indexes and Kubernetes list item names may be
	// exposed. Arbitrary map keys, selector values and rejected values are not.
	diagnosticField = regexp.MustCompile(`^\.?[A-Za-z_][A-Za-z0-9_-]*(?:(?:\.[A-Za-z_][A-Za-z0-9_-]*)|(?:\[[0-9]+\])|(?:\[name="[A-Za-z0-9][A-Za-z0-9_.-]*"\]))*$`)
	schemaField     = regexp.MustCompile(`(?:^|: |\n)(\.[^\n:]+): field not declared in schema`)
	unknownField    = regexp.MustCompile(`unknown field "([^"\n]+)"`)
)

func safeDiagnosticField(field string) string {
	if len(field) <= 1024 && diagnosticField.MatchString(field) {
		return field
	}
	return ""
}

func kubernetesFailureCauses(err error) []store.ApplicationStatusCause {
	var causes []store.ApplicationStatusCause
	add := func(field, reason, message string) {
		if len(causes) >= 20 {
			return
		}
		cause := store.ApplicationStatusCause{Field: safeDiagnosticField(field), Reason: reason, Message: message}
		for _, existing := range causes {
			if existing == cause {
				return
			}
		}
		causes = append(causes, cause)
	}
	var status apierrors.APIStatus
	if errors.As(err, &status) && status.Status().Details != nil {
		for _, cause := range status.Status().Details.Causes {
			reason := string(cause.Type)
			message := "Kubernetes rejected this field."
			switch reason {
			case "FieldValueRequired":
				message = "A required field is missing."
			case "FieldValueNotFound":
				message = "The referenced value was not found."
			case "FieldValueDuplicate":
				message = "The field contains a duplicate value."
			case "FieldValueNotSupported":
				message = "The field has an unsupported value."
			case "FieldValueForbidden":
				message = "This field or change is forbidden."
			case "FieldValueTooLong", "FieldValueTooMany":
				message = "The field exceeds Kubernetes limits."
			case "FieldManagerConflict":
				message = "Another field manager owns this field."
			case "FieldValueInvalid", "FieldValueTypeInvalid":
				message = "The field has an invalid value or type."
			default:
				reason = "FieldRejected"
			}
			if strings.Contains(strings.ToLower(cause.Message), "immutable") {
				reason, message = "FieldValueImmutable", "This field is immutable on the existing resource."
			}
			add(cause.Field, reason, message)
		}
	}
	// SSA schema errors can arrive as plain text (or an InternalError), without
	// Status.Details.Causes. Extract only the field path, not the upstream text.
	for _, match := range schemaField.FindAllStringSubmatch(err.Error(), 20) {
		add(match[1], "FieldNotDeclared", "This field is not declared in the Kubernetes schema.")
	}
	for _, match := range unknownField.FindAllStringSubmatch(err.Error(), 20) {
		add(match[1], "FieldNotDeclared", "This field is not declared in the Kubernetes schema.")
	}
	return causes
}

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
