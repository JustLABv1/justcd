package apphealth

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

type Status string

const (
	Healthy     Status = "Healthy"
	Progressing Status = "Progressing"
	Degraded    Status = "Degraded"
	Suspended   Status = "Suspended"
	Missing     Status = "Missing"
	Unknown     Status = "Unknown"
	Partial     Status = "Partial"
)

type KubernetesCondition struct {
	Type               string     `json:"type"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason,omitempty"`
	Message            string     `json:"message,omitempty"`
	LastTransitionTime *time.Time `json:"lastTransitionTime,omitempty"`
}

// ResourceDetails contains only bounded, non-secret status fields copied from
// Kubernetes. It intentionally does not retain the unstructured Status object.
type ResourceDetails struct {
	StatusObserved     bool                  `json:"statusObserved,omitempty"`
	DesiredReplicas    *int64                `json:"desiredReplicas,omitempty"`
	ReadyReplicas      *int64                `json:"readyReplicas,omitempty"`
	UpdatedReplicas    *int64                `json:"updatedReplicas,omitempty"`
	AvailableReplicas  *int64                `json:"availableReplicas,omitempty"`
	DesiredScheduled   *int64                `json:"desiredScheduled,omitempty"`
	NumberScheduled    *int64                `json:"numberScheduled,omitempty"`
	UpdatedScheduled   *int64                `json:"updatedScheduled,omitempty"`
	NumberReady        *int64                `json:"numberReady,omitempty"`
	NumberAvailable    *int64                `json:"numberAvailable,omitempty"`
	NumberMisscheduled *int64                `json:"numberMisscheduled,omitempty"`
	Completions        *int64                `json:"completions,omitempty"`
	Active             *int64                `json:"active,omitempty"`
	Succeeded          *int64                `json:"succeeded,omitempty"`
	Failed             *int64                `json:"failed,omitempty"`
	Suspended          bool                  `json:"suspended,omitempty"`
	FailureReason      string                `json:"failureReason,omitempty"`
	FailureMessage     string                `json:"failureMessage,omitempty"`
	Conditions         []KubernetesCondition `json:"conditions,omitempty"`
}

type Observation struct {
	Identity  core.Identity   `json:"identity"`
	Source    string          `json:"source"`
	Phase     string          `json:"phase,omitempty"`
	Readiness string          `json:"readiness,omitempty"`
	Details   ResourceDetails `json:"details,omitempty"`
}

type ResourceAssessment struct {
	Identity  core.Identity `json:"identity"`
	Status    Status        `json:"status"`
	Reason    string        `json:"reason"`
	Message   string        `json:"message"`
	Phase     string        `json:"phase,omitempty"`
	Readiness string        `json:"readiness,omitempty"`
}

type ApplicationCondition struct {
	Status             Status               `json:"status"`
	Reason             string               `json:"reason"`
	Message            string               `json:"message"`
	LastTransitionTime time.Time            `json:"lastTransitionTime"`
	ObservedAt         *time.Time           `json:"observedAt,omitempty"`
	Resources          []ResourceAssessment `json:"resources"`
	Warnings           []string             `json:"warnings"`
}

type Transition struct {
	ID        int64                `json:"id"`
	Status    Status               `json:"status"`
	Reason    string               `json:"reason"`
	Message   string               `json:"message"`
	Resources []ResourceAssessment `json:"resources"`
	ChangedAt time.Time            `json:"changedAt"`
}

func SupportedKind(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "Pod":
		return true
	default:
		return false
	}
}

func isCustomResource(apiVersion string) bool {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) != 2 {
		return false // the core v1 API group
	}
	switch parts[0] {
	case "admissionregistration.k8s.io", "apiextensions.k8s.io", "apiregistration.k8s.io",
		"apps", "authentication.k8s.io", "authorization.k8s.io", "autoscaling",
		"batch", "certificates.k8s.io", "coordination.k8s.io", "discovery.k8s.io",
		"events.k8s.io", "extensions", "flowcontrol.apiserver.k8s.io", "networking.k8s.io",
		"node.k8s.io", "policy", "rbac.authorization.k8s.io", "resource.k8s.io",
		"scheduling.k8s.io", "storage.k8s.io", "storagemigration.k8s.io":
		return false
	default:
		return true
	}
}

// Evaluate applies fixed precedence and kind-specific readiness rules. Warnings
// always prevent a Healthy result, and sample observations are never presented
// as live health.
func Evaluate(managed []core.Identity, observed []Observation, warnings []string, suspended bool, now time.Time) ApplicationCondition {
	now = now.UTC()
	condition := ApplicationCondition{Status: Unknown, Reason: "NoHealthEvidence", Message: "No supported Kubernetes workload health has been observed.", LastTransitionTime: now, ObservedAt: &now, Resources: []ResourceAssessment{}, Warnings: normalizeStrings(warnings)}

	live := make(map[string]Observation)
	sampleCount := 0
	for _, item := range observed {
		if item.Source == "sample" {
			sampleCount++
			continue
		}
		if item.Source != "kubernetes" || !SupportedKind(item.Identity.Kind) {
			continue
		}
		live[item.Identity.Key()] = item
	}
	if len(live) == 0 && sampleCount > 0 {
		condition.Reason = "SampleData"
		condition.Message = "Only sample topology data is available; live Kubernetes health cannot be inferred."
		return condition
	}

	unsupported := 0
	managedWorkloads := map[string]core.Identity{}
	unsupportedResources := make([]core.Identity, 0)
	for _, identity := range managed {
		if SupportedKind(identity.Kind) {
			managedWorkloads[identity.Key()] = identity
		} else if isCustomResource(identity.APIVersion) {
			// Built-in resources such as Services and ConfigMaps are not workload
			// health signals. Custom resources remain unassessed so supported
			// workloads cannot make the whole application look healthy.
			unsupported++
			unsupportedResources = append(unsupportedResources, identity)
		}
	}

	assessments := make([]ResourceAssessment, 0, len(live)+len(managedWorkloads))
	for _, item := range live {
		assessments = append(assessments, assess(item))
	}
	for _, identity := range unsupportedResources {
		assessments = append(assessments, ResourceAssessment{
			Identity: identity, Status: Unknown, Reason: "UnsupportedResourceKind",
			Message: "Health checks are not defined for this custom resource kind.",
		})
	}
	if len(condition.Warnings) == 0 {
		for key, identity := range managedWorkloads {
			if _, found := live[key]; !found {
				assessments = append(assessments, ResourceAssessment{
					Identity: identity, Status: Missing, Reason: "ResourceNotObserved",
					Message: "The managed workload was not found in the latest complete Kubernetes observation.",
				})
			}
		}
	}
	sort.Slice(assessments, func(i, j int) bool { return assessments[i].Identity.Key() < assessments[j].Identity.Key() })

	known, progressing, degraded, missing, unknown, resourceSuspended := 0, 0, 0, 0, 0, 0
	for _, item := range assessments {
		switch item.Status {
		case Healthy:
			known++
		case Progressing:
			known++
			progressing++
		case Degraded:
			known++
			degraded++
		case Missing:
			known++
			missing++
		case Suspended:
			known++
			resourceSuspended++
		case Unknown:
			unknown++
		}
	}
	for _, item := range assessments {
		if item.Status != Healthy {
			condition.Resources = append(condition.Resources, item)
		}
	}
	if len(condition.Resources) > 50 {
		condition.Resources = condition.Resources[:50]
	}

	switch {
	case degraded > 0:
		condition.Status, condition.Reason = Degraded, "WorkloadDegraded"
		condition.Message = fmt.Sprintf("%d supported workload resource(s) report a failure.", degraded)
	case missing > 0:
		condition.Status, condition.Reason = Missing, "ManagedWorkloadMissing"
		condition.Message = fmt.Sprintf("%d managed workload resource(s) were not found in the latest complete observation.", missing)
	case len(condition.Warnings) > 0:
		if known > 0 {
			condition.Status, condition.Reason = Partial, "ObservationIncomplete"
			condition.Message = "Some Kubernetes resources could not be read; the available observations are not enough to declare the application healthy."
		} else {
			condition.Status, condition.Reason = Unknown, "HealthObservationUnavailable"
			condition.Message = "Kubernetes health could not be determined because resource observations were unavailable."
		}
	case unsupported > 0 && known == 0:
		condition.Status, condition.Reason = Unknown, "UnsupportedResourceHealth"
		condition.Message = fmt.Sprintf("Health cannot be inferred for %d managed resource(s) of unsupported kinds.", unsupported)
	case unknown > 0 && known == 0:
		condition.Status, condition.Reason = Unknown, "WorkloadHealthUnknown"
		condition.Message = "Kubernetes returned supported workload resources without enough status to determine health."
	case unsupported > 0 || unknown > 0:
		condition.Status, condition.Reason = Partial, "HealthPartiallyInferred"
		condition.Message = "Health is available for only part of the application; some resource kinds or statuses cannot be inferred."
	case suspended:
		condition.Status, condition.Reason = Suspended, "ReconciliationSuspended"
		condition.Message = "Application reconciliation is suspended."
	case resourceSuspended > 0:
		condition.Status, condition.Reason = Suspended, "WorkloadSuspended"
		condition.Message = fmt.Sprintf("%d supported workload resource(s) are suspended.", resourceSuspended)
	case progressing > 0:
		condition.Status, condition.Reason = Progressing, "WorkloadsProgressing"
		condition.Message = fmt.Sprintf("%d supported workload resource(s) are still becoming ready.", progressing)
	case known > 0:
		condition.Status, condition.Reason = Healthy, "AllWorkloadsHealthy"
		condition.Message = "All observed supported workloads satisfy their readiness checks."
	case unsupported > 0:
		condition.Status, condition.Reason = Unknown, "UnsupportedResourceHealth"
		condition.Message = fmt.Sprintf("Health cannot be inferred for %d managed resource(s) of unsupported kinds.", unsupported)
	default:
		condition.Status, condition.Reason = Unknown, "NoManagedWorkloads"
		condition.Message = "No supported managed workloads are available to evaluate."
	}
	return condition
}

func assess(item Observation) ResourceAssessment {
	assessment := ResourceAssessment{Identity: item.Identity, Phase: item.Phase, Readiness: item.Readiness, Status: Unknown, Reason: "StatusUnavailable", Message: "Kubernetes did not report enough status to determine this resource's health."}
	if reason := failureCondition(item.Details.Conditions); reason != nil {
		assessment.Status, assessment.Reason, assessment.Message = Degraded, reason.Reason, reason.Message
		if assessment.Reason == "" {
			assessment.Reason = reason.Type
		}
		if assessment.Message == "" {
			assessment.Message = reason.Type + " condition reports failure."
		}
		return assessment
	}
	if item.Details.FailureReason != "" {
		assessment.Status, assessment.Reason, assessment.Message = Degraded, item.Details.FailureReason, item.Details.FailureMessage
		if assessment.Message == "" {
			assessment.Message = item.Details.FailureReason
		}
		return assessment
	}

	switch item.Identity.Kind {
	case "Deployment":
		for _, condition := range item.Details.Conditions {
			if condition.Type == "Progressing" && condition.Status == "False" {
				return withStatus(assessment, Degraded, firstNonEmpty(condition.Reason, "DeploymentNotProgressing"), firstNonEmpty(condition.Message, "The Deployment controller reports that rollout progress stopped."))
			}
		}
		desired := valueOr(item.Details.DesiredReplicas, 1)
		if desired == 0 {
			return withStatus(assessment, Healthy, "ScaledToZero", "The Deployment is intentionally scaled to zero replicas.")
		}
		if item.Details.StatusObserved {
			ready := valueOr(item.Details.ReadyReplicas, 0)
			if ready < desired || valueOr(item.Details.UpdatedReplicas, 0) < desired || valueOr(item.Details.AvailableReplicas, 0) < desired {
				return withStatus(assessment, Progressing, "DeploymentReplicasNotReady", fmt.Sprintf("Deployment has %d ready of %d desired replicas.", ready, desired))
			}
			return withStatus(assessment, Healthy, "DeploymentAvailable", fmt.Sprintf("Deployment has %d ready of %d desired replicas.", ready, desired))
		}
		if conditionTrue(item.Details.Conditions, "Available") && conditionTrue(item.Details.Conditions, "Progressing") {
			return withStatus(assessment, Healthy, "DeploymentAvailable", "Deployment reports Available and Progressing conditions as true.")
		}
	case "StatefulSet":
		desired := valueOr(item.Details.DesiredReplicas, 1)
		if desired == 0 {
			return withStatus(assessment, Healthy, "ScaledToZero", "The StatefulSet is intentionally scaled to zero replicas.")
		}
		if item.Details.StatusObserved {
			ready := valueOr(item.Details.ReadyReplicas, 0)
			if ready < desired || valueOr(item.Details.UpdatedReplicas, 0) < desired {
				return withStatus(assessment, Progressing, "StatefulSetReplicasNotReady", fmt.Sprintf("StatefulSet has %d ready of %d desired replicas.", ready, desired))
			}
			return withStatus(assessment, Healthy, "StatefulSetReady", fmt.Sprintf("StatefulSet has %d ready of %d desired replicas.", ready, desired))
		}
	case "DaemonSet":
		if item.Details.StatusObserved {
			desired := valueOr(item.Details.DesiredScheduled, 0)
			if desired == 0 {
				return withStatus(assessment, Healthy, "NoNodesSelected", "The DaemonSet has no eligible nodes.")
			}
			ready := valueOr(item.Details.NumberReady, 0)
			if ready < desired || valueOr(item.Details.UpdatedScheduled, 0) < desired || valueOr(item.Details.NumberMisscheduled, 0) > 0 {
				return withStatus(assessment, Progressing, "DaemonSetPodsNotReady", fmt.Sprintf("DaemonSet has %d ready of %d desired pods.", ready, desired))
			}
			return withStatus(assessment, Healthy, "DaemonSetReady", fmt.Sprintf("DaemonSet has %d ready of %d desired pods.", ready, desired))
		}
	case "Job":
		if item.Details.Suspended {
			return withStatus(assessment, Suspended, "JobSuspended", "The Job is suspended by its spec.")
		}
		if valueOr(item.Details.Failed, 0) > 0 {
			return withStatus(assessment, Degraded, "JobFailed", fmt.Sprintf("Job reports %d failed pod(s).", *item.Details.Failed))
		}
		if conditionTrue(item.Details.Conditions, "Complete") || valueOr(item.Details.Succeeded, 0) >= valueOr(item.Details.Completions, 1) {
			return withStatus(assessment, Healthy, "JobComplete", "Job has completed its requested work.")
		}
		if item.Details.StatusObserved || item.Details.Active != nil || item.Details.Succeeded != nil || item.Details.Completions != nil {
			return withStatus(assessment, Progressing, "JobActive", fmt.Sprintf("Job has %d active pod(s) and %d successful completion(s).", valueOr(item.Details.Active, 0), valueOr(item.Details.Succeeded, 0)))
		}
	case "Pod":
		switch strings.ToLower(item.Phase) {
		case "succeeded":
			return withStatus(assessment, Healthy, "PodSucceeded", "Pod completed successfully.")
		case "failed":
			return withStatus(assessment, Degraded, "PodFailed", "Pod phase is Failed.")
		case "pending":
			return withStatus(assessment, Progressing, "PodPending", firstNonEmpty(conditionMessage(item.Details.Conditions, "PodScheduled"), "Pod is waiting to be scheduled or initialized."))
		case "running":
			if item.Readiness == "Ready" {
				return withStatus(assessment, Healthy, "PodReady", "Pod Ready condition is true.")
			}
			if item.Readiness == "Not ready" {
				return withStatus(assessment, Progressing, "PodNotReady", firstNonEmpty(conditionMessage(item.Details.Conditions, "Ready"), "Pod is running but its Ready condition is false."))
			}
			if item.Readiness == "Unknown" {
				return withStatus(assessment, Unknown, "PodReadinessUnknown", firstNonEmpty(conditionMessage(item.Details.Conditions, "Ready"), "Pod Ready condition is unknown."))
			}
		}
	}
	return assessment
}

func withStatus(item ResourceAssessment, status Status, reason, message string) ResourceAssessment {
	item.Status, item.Reason, item.Message = status, reason, message
	return item
}

func failureCondition(conditions []KubernetesCondition) *KubernetesCondition {
	for i := range conditions {
		condition := &conditions[i]
		if condition.Status == "True" && (condition.Type == "Failed" || condition.Type == "ReplicaFailure") {
			return condition
		}
	}
	return nil
}

func conditionTrue(conditions []KubernetesCondition, typ string) bool {
	for _, condition := range conditions {
		if condition.Type == typ && condition.Status == "True" {
			return true
		}
	}
	return false
}

func conditionMessage(conditions []KubernetesCondition, typ string) string {
	for _, condition := range conditions {
		if condition.Type == typ {
			return condition.Message
		}
	}
	return ""
}

func valueOr(value *int64, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
