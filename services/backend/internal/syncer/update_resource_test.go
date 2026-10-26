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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

// Exercise the actual apply path, stopping before the managed-resource database write.
func TestUpdateResourceRechecksReviewedContentAndUsesCurrentVersion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*unstructured.Unstructured)
		wantPatch bool
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
			change := core.Change{Kind: core.Update, Identity: reviewed.Identity, LiveUID: reviewed.UID, LiveResourceVersion: reviewed.ResourceVersion, LiveFingerprint: reviewed.Fingerprint, Before: reviewed.Manifest}
			current := before.DeepCopy()
			tc.mutate(current)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), current)
			requested := false
			stop := errors.New("test stopped at PATCH")
			client.PrependReactor("patch", "poddisruptionbudgets", func(action ktesting.Action) (bool, runtime.Object, error) {
				requested = true
				patch := &unstructured.Unstructured{}
				if err := patch.UnmarshalJSON(action.(ktesting.PatchAction).GetPatch()); err != nil {
					t.Fatal(err)
				}
				if patch.GetResourceVersion() != current.GetResourceVersion() {
					t.Fatalf("PATCH version=%q, want freshly checked %q", patch.GetResourceVersion(), current.GetResourceVersion())
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
			err = (&Service{}).applyResource(context.Background(), input, "app", reviewed, change)
			if requested != tc.wantPatch {
				t.Fatalf("PATCH requested=%v, want %v; error=%v", requested, tc.wantPatch, err)
			}
			if tc.wantPatch && !errors.Is(err, stop) {
				t.Fatalf("expected API error to propagate, got %v", err)
			}
			if !tc.wantPatch && (err == nil || !strings.Contains(err.Error(), "changed after plan review")) {
				t.Fatalf("expected review safeguard, got %v", err)
			}
		})
	}
}
