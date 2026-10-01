package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ObserveTopology caches only controller-created descendants of directly
// managed resources. Listing requires read access, never write access.
func (s *Service) ObserveTopology(ctx context.Context, app store.Application, managed []store.ManagedResource) error {
	cluster, err := s.Store.ClusterByID(ctx, app.ClusterID)
	if err != nil {
		return err
	}
	if parsed, err := url.Parse(cluster.APIServer); err == nil && strings.HasSuffix(parsed.Hostname(), ".invalid") {
		return errors.New("demo cluster is intentionally unreachable; showing sample observations")
	}
	known := map[string]bool{}
	managedUIDs := map[string]bool{}
	for _, item := range managed {
		if item.UID != "" {
			known[item.UID] = true
			managedUIDs[item.UID] = true
		}
	}
	if len(known) == 0 {
		return s.Store.ReplaceObservedResources(ctx, app.ID, nil)
	}
	all := []store.ObservedResource{}
	warnings := []string{}
	workspaceCredentialID, err := s.Store.WorkspaceClusterCredential(ctx, app.WorkspaceID, app.ClusterID)
	if err != nil {
		return err
	}
	for _, binding := range app.Namespaces {
		stored, err := s.Store.NamespaceBinding(ctx, app.WorkspaceID, app.ClusterID, binding.Namespace)
		if err != nil {
			return err
		}
		credentialID := stored.CredentialID
		if credentialID == nil {
			credentialID = workspaceCredentialID
		}
		client, err := kube.ForWorkspaceBinding(ctx, s.Store, s.EncryptionKey, cluster, credentialID, false, app.WorkspaceID, stored.Namespace)
		if err != nil {
			return err
		}
		for _, target := range []struct {
			gvr  schema.GroupVersionResource
			kind string
		}{
			{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "Deployment"},
			{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, "StatefulSet"},
			{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, "DaemonSet"},
			{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, "ReplicaSet"},
			{schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, "Job"},
			{schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "Pod"},
		} {
			listCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			result, listErr := client.Dynamic.Resource(target.gvr).Namespace(binding.Namespace).List(listCtx, metav1.ListOptions{Limit: 500})
			cancel()
			if listErr != nil {
				warnings = append(warnings, fmt.Sprintf("cannot observe %s in %s", target.kind, binding.Namespace))
				continue
			}
			if result.GetContinue() != "" {
				warnings = append(warnings, fmt.Sprintf("more than 500 %s in %s; topology is partial", target.kind, binding.Namespace))
			}
			for _, object := range result.Items {
				object.SetAPIVersion(target.gvr.GroupVersion().String())
				object.SetKind(target.kind)
				all = append(all, observedObject(app.ClusterID, object))
			}
		}
	}
	selected := []store.ObservedResource{}
	selectedUIDs := map[string]bool{}
	for _, item := range all {
		if item.UID != "" && managedUIDs[item.UID] {
			selected = append(selected, item)
			selectedUIDs[item.UID] = true
		}
	}
	for range 4 {
		added := false
		for _, item := range all {
			if item.UID == "" || known[item.UID] || selectedUIDs[item.UID] {
				continue
			}
			for _, owner := range item.OwnerUIDs {
				if known[owner] {
					known[item.UID] = true
					selected = append(selected, item)
					selectedUIDs[item.UID] = true
					added = true
					break
				}
			}
		}
		if !added {
			break
		}
	}
	if err := s.Store.ReplaceObservedResources(ctx, app.ID, selected); err != nil {
		return err
	}
	if len(warnings) > 0 {
		return errors.New(strings.Join(warnings, "; "))
	}
	return nil
}

// RefreshApplicationHealth stores a deterministic application-level condition
// from the latest managed-resource and descendant observations. Read failures
// are persisted as partial/unknown health instead of leaving a stale Healthy
// result in place.
func (s *Service) RefreshApplicationHealth(ctx context.Context, app store.Application) error {
	managed, err := s.Store.ManagedResources(ctx, app.ID)
	if err != nil {
		return err
	}
	observeErr := s.ObserveTopology(ctx, app, managed)
	observed, err := s.Store.ObservedResources(ctx, app.ID)
	if err != nil {
		return err
	}
	identities := make([]core.Identity, 0, len(managed))
	for _, item := range managed {
		identities = append(identities, item.Identity)
	}
	resources := make([]apphealth.Observation, 0, len(observed))
	for _, item := range observed {
		resources = append(resources, apphealth.Observation{
			Identity:  item.Identity,
			Source:    item.Source,
			Phase:     item.Phase,
			Readiness: item.Readiness,
			Details:   item.HealthSummary,
		})
	}
	warnings := []string{}
	if observeErr != nil && !strings.Contains(observeErr.Error(), "demo cluster is intentionally unreachable") {
		warnings = append(warnings, "Some Kubernetes resources could not be read; health may be partial.")
	}
	condition := apphealth.Evaluate(identities, resources, warnings, app.AutoSyncPaused || app.Decommissioning, time.Now().UTC())
	if err := s.Store.RecordApplicationHealthCondition(ctx, app.ID, condition); err != nil {
		return err
	}
	return observeErr
}

func observedObject(clusterID string, object unstructured.Unstructured) store.ObservedResource {
	owners := make([]string, 0, len(object.GetOwnerReferences()))
	for _, owner := range object.GetOwnerReferences() {
		owners = append(owners, string(owner.UID))
	}
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	details := observedHealthSummary(object)
	readiness := ""
	if object.GetKind() == "Pod" {
		for _, condition := range details.Conditions {
			if condition.Type != "Ready" {
				continue
			}
			switch condition.Status {
			case "True":
				readiness = "Ready"
			case "False":
				readiness = "Not ready"
			case "Unknown":
				readiness = "Unknown"
			}
			break
		}
	}
	return store.ObservedResource{
		Identity: coreIdentity(clusterID, object), UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(),
		Labels: object.GetLabels(), OwnerUIDs: owners, Phase: phase, Readiness: readiness, HealthSummary: details, Source: "kubernetes", ObservedAt: time.Now().UTC(),
	}
}

func observedHealthSummary(object unstructured.Unstructured) apphealth.ResourceDetails {
	details := apphealth.ResourceDetails{Conditions: []apphealth.KubernetesCondition{}}
	_, details.StatusObserved, _ = unstructured.NestedMap(object.Object, "status")
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, raw := range conditions {
		if len(details.Conditions) >= 32 {
			break
		}
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := condition["type"].(string)
		status, _ := condition["status"].(string)
		if typ == "" || status == "" {
			continue
		}
		item := apphealth.KubernetesCondition{Type: boundedStatusText(typ, 128), Status: boundedStatusText(status, 32)}
		if reason, ok := condition["reason"].(string); ok {
			item.Reason = boundedStatusText(reason, 256)
		}
		if message, ok := condition["message"].(string); ok {
			item.Message = boundedStatusText(message, 2048)
		}
		if rawTime, ok := condition["lastTransitionTime"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, rawTime); err == nil {
				parsed = parsed.UTC()
				item.LastTransitionTime = &parsed
			}
		}
		details.Conditions = append(details.Conditions, item)
	}

	switch object.GetKind() {
	case "Deployment", "StatefulSet":
		details.DesiredReplicas = nestedInt64(object.Object, "spec", "replicas")
		details.ReadyReplicas = nestedInt64(object.Object, "status", "readyReplicas")
		details.UpdatedReplicas = nestedInt64(object.Object, "status", "updatedReplicas")
		details.AvailableReplicas = nestedInt64(object.Object, "status", "availableReplicas")
	case "DaemonSet":
		details.DesiredScheduled = nestedInt64(object.Object, "status", "desiredNumberScheduled")
		details.NumberScheduled = nestedInt64(object.Object, "status", "currentNumberScheduled")
		details.UpdatedScheduled = nestedInt64(object.Object, "status", "updatedNumberScheduled")
		details.NumberReady = nestedInt64(object.Object, "status", "numberReady")
		details.NumberAvailable = nestedInt64(object.Object, "status", "numberAvailable")
		details.NumberMisscheduled = nestedInt64(object.Object, "status", "numberMisscheduled")
	case "Job":
		details.Completions = nestedInt64(object.Object, "spec", "completions")
		details.Active = nestedInt64(object.Object, "status", "active")
		details.Succeeded = nestedInt64(object.Object, "status", "succeeded")
		details.Failed = nestedInt64(object.Object, "status", "failed")
		details.Suspended, _, _ = unstructured.NestedBool(object.Object, "spec", "suspend")
	case "Pod":
		details.FailureReason, details.FailureMessage = podContainerFailure(object.Object)
	}
	return details
}

func nestedInt64(object map[string]any, fields ...string) *int64 {
	value, found, err := unstructured.NestedInt64(object, fields...)
	if err != nil || !found {
		return nil
	}
	return &value
}

func podContainerFailure(object map[string]any) (string, string) {
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		statuses, _, _ := unstructured.NestedSlice(object, "status", field)
		for _, raw := range statuses {
			status, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if reason, found, _ := unstructured.NestedString(status, "state", "waiting", "reason"); found && isContainerFailure(reason) {
				message, _, _ := unstructured.NestedString(status, "state", "waiting", "message")
				return boundedStatusText(reason, 256), boundedStatusText(message, 2048)
			}
			if exitCode, found, _ := unstructured.NestedInt64(status, "state", "terminated", "exitCode"); found && exitCode != 0 {
				reason, _, _ := unstructured.NestedString(status, "state", "terminated", "reason")
				message, _, _ := unstructured.NestedString(status, "state", "terminated", "message")
				if reason == "" {
					reason = "ContainerTerminated"
				}
				return boundedStatusText(reason, 256), boundedStatusText(message, 2048)
			}
		}
	}
	return "", ""
}

func isContainerFailure(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "RunContainerError", "InvalidImageName", "CreateContainerError":
		return true
	default:
		return false
	}
}

func boundedStatusText(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

func coreIdentity(clusterID string, object unstructured.Unstructured) core.Identity {
	return core.Identity{ClusterID: clusterID, APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), ClusterScoped: object.GetNamespace() == ""}
}
