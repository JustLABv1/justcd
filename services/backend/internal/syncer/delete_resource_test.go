package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/render"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

// Exercise the actual deletion path, stopping at the API request before the
// managed-resource database write. Workload deletion causes precisely this PDB
// status/version change during a multi-resource cleanup.
func TestDeleteResourceRechecksReviewedContentAndUsesCurrentVersion(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mutate     func(*unstructured.Unstructured)
		wantDelete bool
	}{
		{"unchanged", func(*unstructured.Unstructured) {}, true},
		{"PDB status changed", func(o *unstructured.Unstructured) {
			o.SetResourceVersion("18")
			o.Object["status"] = map[string]interface{}{"currentHealthy": int64(0), "disruptionsAllowed": int64(0)}
		}, true},
		{"spec changed", func(o *unstructured.Unstructured) {
			o.SetResourceVersion("18")
			_ = unstructured.SetNestedField(o.Object, int64(2), "spec", "minAvailable")
		}, false},
		{"replacement UID", func(o *unstructured.Unstructured) { o.SetUID("replacement") }, false},
		{"ownership changed", func(o *unstructured.Unstructured) {
			o.SetLabels(map[string]string{"justcd.io/application-id": "other"})
		}, false},
		{"finalizer changed", func(o *unstructured.Unstructured) { o.SetFinalizers([]string{"example.com/protect"}) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := &unstructured.Unstructured{}
			if err := before.UnmarshalJSON([]byte(`{"apiVersion":"policy/v1","kind":"PodDisruptionBudget","metadata":{"name":"cache","namespace":"preview","uid":"original","resourceVersion":"17","labels":{"justcd.io/application-id":"app"}},"spec":{"minAvailable":1,"selector":{"matchLabels":{"app":"cache"}}},"status":{"currentHealthy":3}}`)); err != nil {
				t.Fatal(err)
			}
			reviewed, err := render.CanonicalLive(before, "cluster", "app", false)
			if err != nil {
				t.Fatal(err)
			}
			change := core.Change{Kind: core.Delete, Identity: reviewed.Identity, LiveUID: reviewed.UID, LiveResourceVersion: reviewed.ResourceVersion, LiveFingerprint: reviewed.Fingerprint}
			current := before.DeepCopy()
			tc.mutate(current)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), current)
			requested := false
			stop := errors.New("test stopped at DELETE")
			client.PrependReactor("delete", "poddisruptionbudgets", func(action ktesting.Action) (bool, runtime.Object, error) {
				requested = true
				options := action.(interface{ GetDeleteOptions() metav1.DeleteOptions }).GetDeleteOptions()
				if options.Preconditions == nil || options.Preconditions.UID == nil || string(*options.Preconditions.UID) != "original" || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != current.GetResourceVersion() {
					t.Fatalf("DELETE must pin the original UID and freshly checked version: %+v", options)
				}
				if action.GetNamespace() != "preview" {
					t.Fatalf("unexpected namespace %q", action.GetNamespace())
				}
				return true, nil, stop
			})
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "policy", Version: "v1"}})
			mapper.Add(schema.GroupVersionKind{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"}, meta.RESTScopeNamespace)
			clients := &kube.Clients{Dynamic: client, Mapper: mapper}
			input := planInput{Mapper: clients, NamespaceClients: map[string]*kube.Clients{"preview": clients}}
			err = (&Service{}).deleteResource(context.Background(), input, "app", change)
			if requested != tc.wantDelete {
				t.Fatalf("DELETE requested=%v, want %v; error=%v", requested, tc.wantDelete, err)
			}
			if tc.wantDelete && !errors.Is(err, stop) {
				t.Fatalf("expected API error to propagate, got %v", err)
			}
			if !tc.wantDelete && (err == nil || !strings.Contains(err.Error(), "changed after approval")) {
				t.Fatalf("expected review safeguard, got %v", err)
			}
		})
	}
}
