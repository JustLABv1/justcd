package syncer

import (
	"context"
	"errors"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestPlannerDetectsFieldTakeoverUsingDryRunOnly(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary update", true: "field ownership conflict"}[conflict], func(t *testing.T) {
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "apps", Version: "v1"}})
			mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			calls := 0
			client.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				calls++
				options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
				if len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll {
					t.Fatal("planning must never mutate Kubernetes")
				}
				if calls == 1 && options.Force != nil {
					t.Fatal("first dry-run must not force field ownership")
				}
				if conflict && calls == 1 {
					return true, nil, &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonConflict, Code: 409, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict, Field: ".spec.replicas"}}}}}
				}
				if conflict && (options.Force == nil || !*options.Force) {
					t.Fatal("takeover validation must use forced dry-run")
				}
				return true, &unstructured.Unstructured{}, nil
			})
			clients := &kube.Clients{Dynamic: client, Mapper: mapper}
			id := core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "api"}
			plan := core.Plan{ApplicationID: "app", Changes: []core.Change{{Kind: core.Update, Identity: id}}}
			resource := core.Resource{Identity: id, Manifest: []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"team"},"spec":{"replicas":1}}`)}
			err := (&Service{}).detectFieldTakeovers(context.Background(), planInput{Mapper: clients, NamespaceClients: map[string]*kube.Clients{"team": clients}}, &plan, []core.Resource{resource})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Changes[0].Takeover != conflict {
				t.Fatalf("takeover=%v", plan.Changes[0].Takeover)
			}
			expected := 1
			if conflict {
				expected = 2
			}
			if calls != expected {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestResourceVersionConflictIsNotFieldTakeover(t *testing.T) {
	err := apierrors.NewConflict(schema.GroupResource{Resource: "deployments"}, "api", errors.New("resource version changed"))
	if fieldOwnershipConflict(err) {
		t.Fatal("stale resource versions must not enable force")
	}
}
