package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

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
	for _, item := range managed {
		if item.UID != "" {
			known[item.UID] = true
		}
	}
	if len(known) == 0 {
		return nil
	}
	all := []store.ObservedResource{}
	warnings := []string{}
	for _, binding := range app.Namespaces {
		stored, err := s.Store.NamespaceBinding(ctx, app.ProjectID, app.ClusterID, binding.Namespace)
		if err != nil {
			return err
		}
		client, err := kube.ForBinding(ctx, s.Store, s.EncryptionKey, cluster, stored.CredentialID, false)
		if err != nil {
			return err
		}
		for _, target := range []struct {
			gvr  schema.GroupVersionResource
			kind string
		}{
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
	for range 4 {
		added := false
		for _, item := range all {
			if known[item.UID] {
				continue
			}
			for _, owner := range item.OwnerUIDs {
				if known[owner] {
					known[item.UID] = true
					selected = append(selected, item)
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

func observedObject(clusterID string, object unstructured.Unstructured) store.ObservedResource {
	owners := make([]string, 0, len(object.GetOwnerReferences()))
	for _, owner := range object.GetOwnerReferences() {
		owners = append(owners, string(owner.UID))
	}
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	readiness := ""
	if object.GetKind() == "Pod" {
		conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
		for _, raw := range conditions {
			condition, ok := raw.(map[string]any)
			if ok && condition["type"] == "Ready" {
				if condition["status"] == "True" {
					readiness = "Ready"
				} else {
					readiness = "Not ready"
				}
			}
		}
	}
	return store.ObservedResource{
		Identity: coreIdentity(clusterID, object), UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(),
		Labels: object.GetLabels(), OwnerUIDs: owners, Phase: phase, Readiness: readiness, Source: "kubernetes", ObservedAt: time.Now().UTC(),
	}
}

func coreIdentity(clusterID string, object unstructured.Unstructured) core.Identity {
	return core.Identity{ClusterID: clusterID, APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), ClusterScoped: object.GetNamespace() == ""}
}
