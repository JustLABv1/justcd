package syncer

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func namespaceBootstrapInput(openShift bool) (planInput, *dynamicfake.FakeDynamicClient) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	if openShift {
		mapper.AddSpecific(schema.GroupVersionKind{Group: "project.openshift.io", Version: "v1", Kind: "ProjectRequest"}, projectRequestResource, projectRequestResource, meta.RESTScopeRoot)
	}
	input := planInput{Application: store.Application{CreateNamespaces: true}, Cluster: store.Cluster{ID: "cluster"}, NamespaceBindings: map[string]store.NamespaceBinding{"mimir-dev": {Namespace: "mimir-dev"}}, NamespaceClients: map[string]*kube.Clients{"mimir-dev": {Dynamic: client, Mapper: mapper}}}
	return input, client
}

func TestOpenShiftBootstrapUsesWorkloadAccountBeforeReads(t *testing.T) {
	input, client := namespaceBootstrapInput(true)
	privileged := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	input.ClusterScopeClient = &kube.Clients{Dynamic: privileged}
	created := false
	client.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		if !created {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "mimir-distributor", nil)
		}
		return true, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "mimir-distributor", "namespace": "mimir-dev"}}}, nil
	})
	requests := 0
	client.PrependReactor("create", "projectrequests", func(action ktesting.Action) (bool, runtime.Object, error) {
		a := action.(ktesting.CreateActionImpl)
		request := a.GetObject().(*unstructured.Unstructured)
		if request.GetName() != "mimir-dev" || request.GetKind() != "ProjectRequest" || len(a.GetCreateOptions().DryRun) != 0 {
			t.Fatal("unexpected project request", request)
		}
		project := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "project.openshift.io/v1", "kind": "Project", "metadata": map[string]any{"name": "mimir-dev"}}}
		if err := client.Tracker().Create(projectResource, project, ""); err != nil {
			t.Fatal(err)
		}
		// Model OpenShift's project template granting the requesting account
		// project-admin; subsequent workload reads become authorized.
		created = true
		requests++
		return true, project, nil
	})
	ctx := context.Background()
	newNamespace, err := bootstrapNamespace(ctx, input, "mimir-dev")
	if err != nil || !newNamespace || requests != 1 {
		t.Fatalf("project bootstrap failed: %v %v", newNamespace, err)
	}
	deployments := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	if _, err := client.Resource(deployments).Namespace("mimir-dev").Get(ctx, "mimir-distributor", metav1.GetOptions{}); err != nil {
		t.Fatal("workload read happened before requester access was provisioned", err)
	}
	newNamespace, err = bootstrapNamespace(ctx, input, "mimir-dev")
	if err != nil || newNamespace || requests != 1 || len(privileged.Actions()) != 0 {
		t.Fatalf("bootstrap changed identity or recreated project: %v %v", newNamespace, err)
	}
	for _, action := range client.Actions() {
		if action.GetResource() == namespaceResource {
			t.Fatal("OpenShift bootstrap used a bare Namespace instead of ProjectRequest")
		}
	}
}

func TestOpenShiftBootstrapDoesNotBypassDenial(t *testing.T) {
	input, client := namespaceBootstrapInput(true)
	client.PrependReactor("get", "projects", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "project.openshift.io", Resource: "projects"}, "mimir-dev", nil)
	})
	client.PrependReactor("create", "projectrequests", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "project.openshift.io", Resource: "projectrequests"}, "mimir-dev", nil)
	})
	if _, err := bootstrapNamespace(context.Background(), input, "mimir-dev"); !apierrors.IsForbidden(err) {
		t.Fatalf("project request denial ignored: %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetResource() == namespaceResource {
			t.Fatal("denied project request fell back to namespace creation")
		}
	}
}

func TestKubernetesBootstrapIsOptInAndIdempotent(t *testing.T) {
	input, client := namespaceBootstrapInput(false)
	input.Application.CreateNamespaces = false
	if _, err := bootstrapNamespace(context.Background(), input, "mimir-dev"); err == nil || len(client.Actions()) != 0 {
		t.Fatal("disabled bootstrap accessed Kubernetes")
	}
	input.Application.CreateNamespaces = true
	if _, err := bootstrapNamespace(context.Background(), input, "other"); err == nil || len(client.Actions()) != 0 {
		t.Fatal("bootstrap escaped bound namespace")
	}
	created, err := bootstrapNamespace(context.Background(), input, "mimir-dev")
	if err != nil || !created {
		t.Fatal("missing namespace was not created", err)
	}
	namespace, err := client.Resource(namespaceResource).Get(context.Background(), "mimir-dev", metav1.GetOptions{})
	if err != nil || namespace.GetLabels()["justcd.io/application-id"] != "" {
		t.Fatal("bootstrap namespace became an application-owned resource", err)
	}
	client.ClearActions()
	created, err = bootstrapNamespace(context.Background(), input, "mimir-dev")
	if err != nil || created {
		t.Fatal("existing namespace was recreated", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("existing namespace modified", action)
		}
	}
}

func TestRepositoryNamespaceBootstrapRequiresCurrentOwnerPolicy(t *testing.T) {
	id := "app"
	app := store.Application{ID: id, WorkspaceID: "workspace", SourceID: "source", ClusterID: "cluster", CreateNamespaces: true, Revision: "head", Namespaces: []store.NamespaceBinding{{Namespace: "mimir-dev"}}}
	repo := store.RepositoryConfiguration{ID: "repo", WorkspaceID: app.WorkspaceID, SourceID: app.SourceID, Enabled: true, PRSettings: store.RepositoryPRSettings{Enabled: true, Mode: "existing", AllowNamespaceCreation: true, Profile: store.PreviewProfile{ConfirmShared: true}, Destinations: []store.PRDestination{{ClusterID: app.ClusterID, NamespacePattern: ".*-dev"}}}}
	entry := store.RepositoryPRApplication{ApplicationID: &id, RepositoryID: repo.ID, Number: 147, Mode: "existing", HeadSHA: "head"}
	review := store.PullRequestReview{Number: entry.Number, HeadSHA: "head"}
	if !repositoryPRNamespaceBootstrapAllowed(app, repo, entry, review) {
		t.Fatal("authorized bootstrap rejected")
	}
	for _, mode := range []string{"review-only", "isolated"} {
		candidate := repo
		candidate.PRSettings.Mode = mode
		if repositoryPRNamespaceBootstrapAllowed(app, candidate, entry, review) {
			t.Fatal("unsupported mode created namespace", mode)
		}
	}
	for _, mutate := range []func(*store.RepositoryConfiguration){
		func(r *store.RepositoryConfiguration) { r.Enabled = false },
		func(r *store.RepositoryConfiguration) { r.PRSettings.Enabled = false },
		func(r *store.RepositoryConfiguration) { r.PRSettings.AllowNamespaceCreation = false },
		func(r *store.RepositoryConfiguration) { r.PRSettings.Profile.ConfirmShared = false },
		func(r *store.RepositoryConfiguration) { r.WorkspaceID = "other-workspace" },
		func(r *store.RepositoryConfiguration) { r.SourceID = "other-source" },
	} {
		candidate := repo
		mutate(&candidate)
		if repositoryPRNamespaceBootstrapAllowed(app, candidate, entry, review) {
			t.Fatal("bootstrap escaped current owner policy or workspace/source")
		}
	}
	for _, mutate := range []func(*store.RepositoryPRApplication, *store.PullRequestReview){
		func(e *store.RepositoryPRApplication, _ *store.PullRequestReview) { e.DefinitionRemoved = true },
		func(e *store.RepositoryPRApplication, _ *store.PullRequestReview) { e.Merged = true },
		func(_ *store.RepositoryPRApplication, r *store.PullRequestReview) { r.Closed = true },
		func(_ *store.RepositoryPRApplication, r *store.PullRequestReview) { r.Fork = true },
		func(_ *store.RepositoryPRApplication, r *store.PullRequestReview) { r.HeadSHA = "stale" },
	} {
		e, r := entry, review
		mutate(&e, &r)
		if repositoryPRNamespaceBootstrapAllowed(app, repo, e, r) {
			t.Fatal("stale or removed PR bootstrapped namespace")
		}
	}
	app.Namespaces[0].Namespace = "production"
	if repositoryPRNamespaceBootstrapAllowed(app, repo, entry, review) {
		t.Fatal("bootstrap escaped namespace pattern")
	}
}

func TestTerminatingNamespaceIsNotAdoptedByBootstrap(t *testing.T) {
	input, client := namespaceBootstrapInput(false)
	namespace := namespaceObject("mimir-dev")
	now := metav1.Now()
	namespace.SetDeletionTimestamp(&now)
	if err := client.Tracker().Create(namespaceResource, namespace, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrapNamespace(context.Background(), input, "mimir-dev"); err == nil || !strings.Contains(err.Error(), "terminating") {
		t.Fatal("terminating namespace accepted", err)
	}
}

func TestBootstrapLeavesExistingEnvironmentAndExternalRBACUnchanged(t *testing.T) {
	for _, openShift := range []bool{false, true} {
		input, client := namespaceBootstrapInput(openShift)
		resource, kind, apiVersion := namespaceResource, "Namespace", "v1"
		if openShift {
			resource, kind, apiVersion = projectResource, "Project", "project.openshift.io/v1"
		}
		existing := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]any{
			"name": "mimir-dev", "uid": "preexisting-uid", "resourceVersion": "123",
			"labels":      map[string]any{"app.kubernetes.io/managed-by": "another-controller"},
			"annotations": map[string]any{"openshift.io/requester": "different-user", "kyverno.io/user": "original-creator"},
		}}}
		roleBindingResource := schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}
		binding := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "RoleBinding",
			"metadata": map[string]any{"name": "admin", "namespace": "mimir-dev", "labels": map[string]any{"app.kubernetes.io/managed-by": "kyverno"}},
			"roleRef":  map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "admin"},
			"subjects": []any{map[string]any{"kind": "User", "name": "different-user"}},
		}}
		originalEnvironment, originalBinding := existing.DeepCopy(), binding.DeepCopy()
		if err := client.Tracker().Create(resource, existing, ""); err != nil {
			t.Fatal(err)
		}
		if err := client.Tracker().Create(roleBindingResource, binding, "mimir-dev"); err != nil {
			t.Fatal(err)
		}
		created, err := bootstrapNamespace(context.Background(), input, "mimir-dev")
		if err != nil || created {
			t.Fatalf("existing environment bootstrap: %v %v", created, err)
		}
		for _, action := range client.Actions() {
			if action.GetVerb() != "get" || action.GetResource() != resource {
				t.Fatalf("bootstrap changed or accessed unrelated resource: %+v", action)
			}
		}
		currentEnvironment, _ := client.Tracker().Get(resource, "", "mimir-dev")
		currentBinding, _ := client.Tracker().Get(roleBindingResource, "mimir-dev", "admin")
		if !reflect.DeepEqual(currentEnvironment, originalEnvironment) || !reflect.DeepEqual(currentBinding, originalBinding) {
			t.Fatal("bootstrap rewrote namespace ownership or external RBAC")
		}
	}
}

func TestBootstrapDoesNotClaimInaccessibleExistingProject(t *testing.T) {
	input, client := namespaceBootstrapInput(true)
	client.PrependReactor("get", "projects", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(projectResource.GroupResource(), "mimir-dev", nil)
	})
	requests := 0
	client.PrependReactor("create", "projectrequests", func(ktesting.Action) (bool, runtime.Object, error) {
		requests++
		return true, nil, apierrors.NewAlreadyExists(projectResource.GroupResource(), "mimir-dev")
	})
	created, err := bootstrapNamespace(context.Background(), input, "mimir-dev")
	if created || err != nil || requests != 1 {
		t.Fatalf("inaccessible project was claimed: %v %v %d", created, err, requests)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "get" && action.GetResource() == projectResource {
			continue
		}
		if action.GetVerb() == "create" && action.GetResource() == projectRequestResource {
			continue
		}
		t.Fatalf("bootstrap modified existing project or bypassed access denial: %+v", action)
	}
}

func TestBootstrapCreationRaceDoesNotModifyWinningEnvironment(t *testing.T) {
	for _, openShift := range []bool{false, true} {
		input, client := namespaceBootstrapInput(openShift)
		resource, requestResource, kind, apiVersion := namespaceResource, namespaceResource, "Namespace", "v1"
		if openShift {
			resource, requestResource, kind, apiVersion = projectResource, projectRequestResource, "Project", "project.openshift.io/v1"
		}
		winner := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]any{"name": "mimir-dev", "annotations": map[string]any{"openshift.io/requester": "another-user"}}}}
		client.PrependReactor("create", requestResource.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
			if err := client.Tracker().Create(resource, winner.DeepCopy(), ""); err != nil {
				t.Fatal(err)
			}
			return true, nil, apierrors.NewAlreadyExists(resource.GroupResource(), "mimir-dev")
		})
		created, err := bootstrapNamespace(context.Background(), input, "mimir-dev")
		if created || err != nil {
			t.Fatalf("creation race not handled: %v %v", created, err)
		}
		current, _ := client.Tracker().Get(resource, "", "mimir-dev")
		if !reflect.DeepEqual(current, winner) {
			t.Fatal("bootstrap rewrote winning namespace/project")
		}
		for _, action := range client.Actions() {
			if action.GetVerb() != "get" && !(action.GetVerb() == "create" && action.GetResource() == requestResource) {
				t.Fatalf("unexpected write after creation race: %+v", action)
			}
		}
	}
}
