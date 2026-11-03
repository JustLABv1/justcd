package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// Triage categories, most urgent first. They are derived from stored
// observations only, so the ranking is deterministic and works without AI.
const (
	categoryOutage          = "outage"           // a workload has no available replicas
	categoryDegradedServing = "degraded_serving" // still serving, but pods or a rollout are failing
	categoryBlockedDelivery = "blocked_delivery" // sync cannot proceed (cluster, Git, ownership)
	categoryNeedsReview     = "needs_review"     // drift, deletion review or stale state
	categoryPaused          = "paused"           // reconciliation deliberately paused
)

var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
var categorySeverity = map[string]string{categoryOutage: "critical", categoryDegradedServing: "high", categoryBlockedDelivery: "medium", categoryNeedsReview: "medium", categoryPaused: "low"}
var categoryTab = map[string]string{categoryOutage: "topology", categoryDegradedServing: "topology", categoryBlockedDelivery: "overview", categoryNeedsReview: "changes", categoryPaused: "overview"}

type workloadSignal struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Desired   int64    `json:"desired"`
	Ready     int64    `json:"ready"`
	Available int64    `json:"available"`
	Updated   int64    `json:"updated"`
	Problems  []string `json:"problems,omitempty"`
}

type triageSignal struct {
	ApplicationID   string           `json:"applicationId"`
	Name            string           `json:"name"`
	Category        string           `json:"category"`
	Severity        string           `json:"severity"`
	ServingTraffic  *bool            `json:"servingTraffic,omitempty"`
	RolloutStalled  bool             `json:"rolloutStalled,omitempty"`
	Workloads       []workloadSignal `json:"workloads,omitempty"`
	Facts           []string         `json:"facts"`
	Issues          []string         `json:"issues,omitempty"`
	RuntimeStatus   string           `json:"runtimeStatus"`
	SyncStatus      string           `json:"syncStatus"`
	AutoSyncPaused  bool             `json:"autoSyncPaused,omitempty"`
	Tab             string           `json:"tab"`
	attentionNeeded bool
}

// A pod reason that means "this container will not start without a change".
var stuckPodReasons = map[string]bool{
	"ImagePullBackOff": true, "ErrImagePull": true, "InvalidImageName": true, "CrashLoopBackOff": true,
	"CreateContainerConfigError": true, "CreateContainerError": true, "RunContainerError": true,
	"Unschedulable": true, "OOMKilled": true, "Error": true,
}

func attentionNeeded(app store.Application) bool {
	switch app.Health {
	case "out_of_sync", "deletion_pending", "degraded", "failed", "error":
		return true
	}
	switch app.HealthCondition.Status {
	case apphealth.Progressing, apphealth.Degraded, apphealth.Suspended, apphealth.Missing, apphealth.Unknown, apphealth.Partial:
		return true
	}
	return app.ConfigurationMissing || app.AutoSyncPaused || len(app.StatusIssues) > 0
}

func int64Value(value *int64) (int64, bool) {
	if value == nil {
		return 0, false
	}
	return *value, true
}

// classifyApplication turns stored observations into impact facts. It never calls Kubernetes.
func classifyApplication(app store.Application, observed []store.ObservedResource) triageSignal {
	signal := triageSignal{
		ApplicationID: app.ID, Name: app.Name, RuntimeStatus: string(app.HealthCondition.Status), SyncStatus: app.Health,
		AutoSyncPaused: app.AutoSyncPaused, attentionNeeded: attentionNeeded(app), Facts: []string{},
	}
	// Owner chain: Pod -> ReplicaSet -> Deployment, so a failing pod is attributed to its workload.
	byUID := map[string]store.ObservedResource{}
	for _, item := range observed {
		if item.UID != "" {
			byUID[item.UID] = item
		}
	}
	rootOf := func(item store.ObservedResource) store.ObservedResource {
		current := item
		for depth := 0; depth < 4; depth++ {
			var next *store.ObservedResource
			for _, uid := range current.OwnerUIDs {
				if owner, ok := byUID[uid]; ok {
					next = &owner
					break
				}
			}
			if next == nil {
				break
			}
			current = *next
		}
		return current
	}
	workloads := map[string]*workloadSignal{}
	key := func(item store.ObservedResource) string {
		return item.Identity.Kind + "/" + item.Identity.Namespace + "/" + item.Identity.Name
	}
	for _, item := range observed {
		if item.Source != "kubernetes" || !map[string]bool{"Deployment": true, "StatefulSet": true, "DaemonSet": true}[item.Identity.Kind] {
			continue
		}
		health := item.HealthSummary
		desired, ok := int64Value(health.DesiredReplicas)
		ready, _ := int64Value(health.ReadyReplicas)
		available, _ := int64Value(health.AvailableReplicas)
		updated, _ := int64Value(health.UpdatedReplicas)
		if item.Identity.Kind == "DaemonSet" {
			desired, ok = int64Value(health.DesiredScheduled)
			ready, _ = int64Value(health.NumberReady)
			available, _ = int64Value(health.NumberAvailable)
			updated, _ = int64Value(health.UpdatedScheduled)
		}
		if !ok {
			continue
		}
		workloads[key(item)] = &workloadSignal{Kind: item.Identity.Kind, Name: item.Identity.Name, Desired: desired, Ready: ready, Available: available, Updated: updated}
	}
	for _, item := range observed {
		if item.Identity.Kind != "Pod" || item.Source != "kubernetes" || item.Phase == "Succeeded" {
			continue
		}
		reason := item.HealthSummary.WaitingReason
		if reason == "" {
			reason = item.HealthSummary.FailureReason
		}
		if reason == "" && item.Phase == "Pending" {
			for _, condition := range item.HealthSummary.Conditions {
				if condition.Type == "PodScheduled" && condition.Status == "False" {
					reason = "Unschedulable"
				}
			}
		}
		if reason == "" || (!stuckPodReasons[reason] && item.Readiness == "Ready") {
			continue
		}
		root := rootOf(item)
		target, ok := workloads[key(root)]
		if !ok {
			continue
		}
		problem := fmt.Sprintf("pod %s: %s", item.Identity.Name, reason)
		if item.HealthSummary.RestartCount > 0 {
			problem += fmt.Sprintf(" (%d restarts)", item.HealthSummary.RestartCount)
		}
		if len(target.Problems) < 4 {
			target.Problems = append(target.Problems, problem)
		}
	}
	names := make([]string, 0, len(workloads))
	for name := range workloads {
		names = append(names, name)
	}
	sort.Strings(names)
	serving, anyDesired := false, false
	for _, name := range names {
		w := workloads[name]
		signal.Workloads = append(signal.Workloads, *w)
		if w.Desired == 0 {
			continue
		}
		anyDesired = true
		switch {
		case w.Available == 0:
			signal.Facts = append(signal.Facts, fmt.Sprintf("%s %s has 0/%d replicas available", w.Kind, w.Name, w.Desired))
		case w.Available < w.Desired || w.Updated < w.Desired:
			serving = true
			signal.RolloutStalled = signal.RolloutStalled || len(w.Problems) > 0 || w.Updated < w.Desired
			signal.Facts = append(signal.Facts, fmt.Sprintf("%s %s serves with %d/%d available, %d/%d updated", w.Kind, w.Name, w.Available, w.Desired, w.Updated, w.Desired))
		default:
			serving = true
			if len(w.Problems) > 0 {
				signal.RolloutStalled = true
				signal.Facts = append(signal.Facts, fmt.Sprintf("%s %s is fully available but has failing pods", w.Kind, w.Name))
			}
		}
		for _, problem := range w.Problems {
			signal.Facts = append(signal.Facts, problem)
		}
	}
	if anyDesired {
		signal.ServingTraffic = &serving
	}
	for _, issue := range app.StatusIssues {
		signal.Issues = append(signal.Issues, redactAPIErrorMessage(issue.Summary))
	}
	if len(app.HealthCondition.Resources) > 0 {
		signal.Facts = append(signal.Facts, fmt.Sprintf("%d resources are reported as needing attention", len(app.HealthCondition.Resources)))
	}
	outage := false
	for _, w := range signal.Workloads {
		if w.Desired > 0 && w.Available == 0 {
			outage = true
		}
	}
	switch {
	case outage:
		signal.Category = categoryOutage
	case signal.RolloutStalled || app.HealthCondition.Status == apphealth.Degraded:
		signal.Category = categoryDegradedServing
	case len(app.StatusIssues) > 0:
		signal.Category = categoryBlockedDelivery
	case app.AutoSyncPaused && !app.ConfigurationMissing:
		signal.Category = categoryPaused
	default:
		signal.Category = categoryNeedsReview
	}
	signal.Severity = categorySeverity[signal.Category]
	signal.Tab = categoryTab[signal.Category]
	if signal.Category == categoryDegradedServing && signal.ServingTraffic != nil && !*signal.ServingTraffic {
		signal.Category, signal.Severity = categoryOutage, "critical"
	}
	if app.Decommissioning || app.ConfigurationMissing {
		signal.Facts = append(signal.Facts, "application is being removed from Git or decommissioned")
		signal.Tab = "changes"
	}
	if app.AutoSyncPaused {
		signal.Facts = append(signal.Facts, "auto-sync is paused")
	}
	if len(signal.Facts) == 0 {
		signal.Facts = append(signal.Facts, fmt.Sprintf("sync %s, runtime %s", strings.ReplaceAll(app.Health, "_", " "), strings.ToLower(string(app.HealthCondition.Status))))
	}
	return signal
}

func sortSignals(signals []triageSignal) {
	sort.SliceStable(signals, func(i, j int) bool {
		if a, b := severityRank[signals[i].Severity], severityRank[signals[j].Severity]; a != b {
			return a < b
		}
		return signals[i].Name < signals[j].Name
	})
}
