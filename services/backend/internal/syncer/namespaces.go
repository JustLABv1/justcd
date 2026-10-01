package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var namespaceResource = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}

// Namespace prerequisites are reviewed changes, but are not application-owned
// resources: shared namespaces must never become prune/decommission targets.
func namespaceProvisioningClient(input planInput, name string) (*kube.Clients, error) {
	if _, ok := input.NamespaceBindings[name]; !ok {
		return nil, fmt.Errorf("namespace %q is not bound to this workspace", name)
	}
	if input.ClusterScopeClient != nil {
		return input.ClusterScopeClient, nil
	}
	client := input.NamespaceClients[name]
	if client == nil {
		return nil, fmt.Errorf("namespace %q has no credential", name)
	}
	return client, nil
}

func addNamespaceCreations(ctx context.Context, input planInput, plan *core.Plan, desired []core.Resource) error {
	if !input.Application.CreateNamespaces {
		return nil
	}
	explicit := map[string]bool{}
	for _, resource := range desired {
		if resource.Identity.APIVersion == "v1" && resource.Identity.Kind == "Namespace" {
			explicit[resource.Identity.Name] = true
		}
	}
	names := make([]string, 0, len(input.NamespaceBindings))
	for name := range input.NamespaceBindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if explicit[name] {
			continue
		}
		client, err := namespaceProvisioningClient(input, name)
		if err != nil {
			return err
		}
		resource := client.Dynamic.Resource(namespaceResource)
		existing, err := resource.Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			if existing.GetDeletionTimestamp() != nil {
				return fmt.Errorf("namespace %q is terminating", name)
			}
			continue
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("check target namespace %q: %w", name, err)
		}
		object := namespaceObject(name)
		if _, err := resource.Create(ctx, object, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); err != nil {
			return fmt.Errorf("namespace %q creation is not permitted: %w", name, err)
		}
		manifest, _ := json.Marshal(object.Object)
		sum := sha256.Sum256(manifest)
		id := core.Identity{ClusterID: input.Cluster.ID, APIVersion: "v1", Kind: "Namespace", Name: name, ClusterScoped: true}
		plan.NamespaceCreations = append(plan.NamespaceCreations, id)
		plan.Changes = append(plan.Changes, core.Change{Kind: core.Create, Identity: id, After: manifest, DesiredFingerprint: hex.EncodeToString(sum[:])})
	}
	return nil
}

func namespaceObject(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": name}}}
}

func createNamespacePrerequisite(ctx context.Context, resource dynamic.ResourceInterface, name string) error {
	// A shared namespace may have appeared since review. Do not take ownership or
	// modify it. Admission/RBAC are still enforced for the actual creation.
	existing, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if existing.GetDeletionTimestamp() != nil {
			return fmt.Errorf("namespace %q is terminating", name)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = resource.Create(ctx, namespaceObject(name), metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := resource.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if existing.GetDeletionTimestamp() != nil {
			return fmt.Errorf("namespace %q is terminating", name)
		}
		return nil
	}
	return err
}
