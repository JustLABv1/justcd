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
	} else {
		issue := PlanFailureDiagnostic(err)
		if issue.Code != "kubernetes.permission_denied" || issue.Resource == nil || issue.Resource.Kind != "Namespace" || issue.Resource.Name != "shop" || issue.Action != "get" {
			t.Fatalf("namespace RBAC context lost: %+v", issue)
		}
	}
	if _, err := namespaceProvisioningClient(input, "unbound"); err == nil {
		t.Fatal("unbound namespace allowed")
	}
}

func TestOpenShiftPlanningBootstrapsProjectWithWorkloadCredential(t *testing.T) {
	input, client := namespaceBootstrapInput(true)
	privileged := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	input.ClusterScopeClient = &kube.Clients{Dynamic: privileged}
	client.PrependReactor("get", "projects", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(projectResource.GroupResource(), "mimir-dev", nil)
	})
	requests := 0
	client.PrependReactor("create", "projectrequests", func(action ktesting.Action) (bool, runtime.Object, error) {
		requests++
		a := action.(ktesting.CreateActionImpl)
		if len(a.GetCreateOptions().DryRun) != 0 {
			t.Fatal("ProjectRequest must not pretend to support dry-run")
		}
		return true, a.GetObject(), nil
	})
	plan := core.Plan{}
	if err := addNamespaceCreations(context.Background(), input, &plan, nil); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(plan.NamespaceCreations) != 0 || len(plan.Changes) != 0 || len(privileged.Actions()) != 0 {
		t.Fatalf("unexpected bootstrap: requests=%d plan=%+v privileged=%v", requests, plan, privileged.Actions())
	}
	for _, action := range client.Actions() {
		if action.GetResource() == namespaceResource {
			t.Fatal("OpenShift planning used bare Namespace API")
		}
	}
}

func TestOpenShiftPlanningPreservesBootstrapPolicyAndDenials(t *testing.T) {
	input, client := namespaceBootstrapInput(true)
	input.Application.CreateNamespaces = false
	if err := addNamespaceCreations(context.Background(), input, &core.Plan{}, nil); err != nil || len(client.Actions()) != 0 {
		t.Fatal("disabled namespace creation contacted OpenShift", err)
	}
	input.Application.CreateNamespaces = true
	input.AuthorizeNamespaceBootstrap = func(context.Context) error {
		return apierrors.NewForbidden(projectRequestResource.GroupResource(), "mimir-dev", nil)
	}
	if err := addNamespaceCreations(context.Background(), input, &core.Plan{}, nil); !apierrors.IsForbidden(err) || len(client.Actions()) != 0 {
		t.Fatalf("bootstrap authorization bypassed: %v actions=%v", err, client.Actions())
	}
	input.AuthorizeNamespaceBootstrap = nil
	client.PrependReactor("create", "projectrequests", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(projectRequestResource.GroupResource(), "mimir-dev", nil)
	})
	err := addNamespaceCreations(context.Background(), input, &core.Plan{}, nil)
	issue := PlanFailureDiagnostic(err)
	if issue.Code != "kubernetes.permission_denied" || issue.Resource == nil || issue.Resource.Kind != "ProjectRequest" || issue.Action != "create" {
		t.Fatalf("project request denial lost context: %+v err=%v", issue, err)
	}
	for _, action := range client.Actions() {
		if action.GetResource() == namespaceResource {
			t.Fatal("denied request fell back to Namespace API")
		}
	}
}

func TestOpenShiftPlanningLeavesExistingProjectUntouched(t *testing.T) {
	input, client := namespaceBootstrapInput(true)
	client.PrependReactor("get", "projects", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, namespaceObject("mimir-dev"), nil
	})
	for i := 0; i < 2; i++ {
		plan := core.Plan{}
		if err := addNamespaceCreations(context.Background(), input, &plan, nil); err != nil || len(plan.Changes) != 0 {
			t.Fatalf("existing project planning failed: %+v %v", plan, err)
		}
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" || action.GetResource() != projectResource {
			t.Fatalf("existing project modified: %+v", action)
		}
	}
}
