package syncer

import (
	"context"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestNamespacePrerequisitePlanningAndCreation(t *testing.T) {
	ctx := context.Background()
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dryRuns := 0
	client.PrependReactor("create", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
		a := action.(ktesting.CreateActionImpl)
		if len(a.GetCreateOptions().DryRun) > 0 {
			dryRuns++
			return true, a.GetObject(), nil
		}
		return false, nil, nil
	})
	input := planInput{Application: store.Application{CreateNamespaces: true}, Cluster: store.Cluster{ID: "cluster"}, NamespaceBindings: map[string]store.NamespaceBinding{"shop": {Namespace: "shop"}}, NamespaceClients: map[string]*kube.Clients{"shop": {Dynamic: client}}}
	plan := core.Plan{}
	if err := addNamespaceCreations(ctx, input, &plan, nil); err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || len(plan.NamespaceCreations) != 1 || dryRuns != 1 {
		t.Fatalf("missing reviewed creation: %+v", plan)
	}
	if _, err := client.Resource(namespaceResource).Get(ctx, "shop", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("planning wrote namespace: %v", err)
	}
	if err := createNamespacePrerequisite(ctx, client.Resource(namespaceResource), "shop"); err != nil {
		t.Fatal(err)
	}
	namespace, err := client.Resource(namespaceResource).Get(ctx, "shop", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if namespace.GetLabels()["justcd.io/application-id"] != "" {
		t.Fatal("prerequisite must not be app-owned")
	}
	client.ClearActions()
	again := core.Plan{}
	if err := addNamespaceCreations(ctx, input, &again, nil); err != nil || len(again.Changes) != 0 {
		t.Fatalf("existing namespace changed: %+v %v", again, err)
	}
	if err := createNamespacePrerequisite(ctx, client.Resource(namespaceResource), "shop"); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("existing namespace mutated: %v", action)
		}
	}
}

func TestNamespaceCreationOptInAndPermissionFailure(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	input := planInput{Application: store.Application{}, Cluster: store.Cluster{ID: "cluster"}, NamespaceBindings: map[string]store.NamespaceBinding{"shop": {Namespace: "shop"}}, NamespaceClients: map[string]*kube.Clients{"shop": {Dynamic: client}}}
	if err := addNamespaceCreations(context.Background(), input, &core.Plan{}, nil); err != nil || len(client.Actions()) != 0 {
		t.Fatal("disabled option contacted namespace API")
	}
	input.Application.CreateNamespaces = true
	explicit := []core.Resource{{Identity: core.Identity{APIVersion: "v1", Kind: "Namespace", Name: "shop"}}}
	if err := addNamespaceCreations(context.Background(), input, &core.Plan{}, explicit); err != nil || len(client.Actions()) != 0 {
		t.Fatal("explicit Namespace manifest duplicated")
	}
	client.PrependReactor("get", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "shop", nil)
	})
	if err := addNamespaceCreations(context.Background(), input, &core.Plan{}, nil); err == nil {
		t.Fatal("RBAC failure ignored")
	}
	if _, err := namespaceProvisioningClient(input, "unbound"); err == nil {
		t.Fatal("unbound namespace allowed")
	}
}
