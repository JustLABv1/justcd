package syncer

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/render"
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

// An empty final plan makes the auto-safe poller mark the application synced
// without calling Apply, even when local positional comparison detected drift.
func TestStackGresDryRunPlan(t *testing.T) {
	const liveScripts = `[{"id":0,"sgScript":"db-default"},{"id":1,"sgScript":"init-db-script"}]`
	for _, tc := range []struct {
		name, desiredScripts, predictedScripts string
		conflict, invalid, ignored             bool
		wantChanges                            int
	}{
		{name: "operator default is a no-op for auto-safe", desiredScripts: `[{"id":1,"sgScript":"init-db-script"}]`, predictedScripts: liveScripts},
		{name: "user script update remains actionable", desiredScripts: `[{"id":1,"sgScript":"new-init-script"}]`, predictedScripts: `[{"id":0,"sgScript":"db-default"},{"id":1,"sgScript":"new-init-script"}]`, wantChanges: 1},
		{name: "user script deletion remains actionable", desiredScripts: `[]`, predictedScripts: `[{"id":0,"sgScript":"db-default"}]`, wantChanges: 1},
		{name: "admission modifies explicitly desired value", desiredScripts: `[{"id":1,"sgScript":"new-init-script"}]`, predictedScripts: liveScripts},
		{name: "no-op ownership transfer still requires review", desiredScripts: `[{"id":1,"sgScript":"init-db-script"}]`, predictedScripts: liveScripts, conflict: true, wantChanges: 1},
		{name: "ignored fields stay out of the server diff", desiredScripts: `[{"id":1,"sgScript":"init-db-script"}]`, predictedScripts: liveScripts, ignored: true},
		{name: "invalid dry-run fails closed", desiredScripts: `[{"id":1,"sgScript":"init-db-script"}]`, predictedScripts: liveScripts, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := core.Identity{ClusterID: "cluster", APIVersion: "stackgres.io/v1", Kind: "SGCluster", Namespace: "team", Name: "db"}
			manifest := func(scripts string) json.RawMessage {
				return json.RawMessage(`{"apiVersion":"stackgres.io/v1","kind":"SGCluster","metadata":{"name":"db","namespace":"team","labels":{"justcd.io/application-id":"app"}},"spec":{"managedSql":{"scripts":` + scripts + `}}}`)
			}
			desired := core.Resource{Identity: id, Manifest: manifest(tc.desiredScripts)}
			original := string(desired.Manifest)
			object := &unstructured.Unstructured{}
			if err := object.UnmarshalJSON(manifest(liveScripts)); err != nil {
				t.Fatal(err)
			}
			object.SetUID("uid")
			object.SetResourceVersion("12")
			live, fingerprint, err := render.CanonicalLiveAgainst(object, "cluster", "app", false, desired.Manifest, manifest(`[{"id":1,"sgScript":"init-db-script"}]`))
			if err != nil {
				t.Fatal(err)
			}
			desired.Fingerprint = fingerprint
			plan, err := core.BuildPlan("app", "commit", []core.Binding{{ClusterID: "cluster", Namespace: "team", CredentialRef: "credential"}}, []core.Resource{desired}, []core.Resource{live})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Changes) != 1 {
				t.Fatalf("fixture must reproduce local drift: %+v", plan.Changes)
			}
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "stackgres.io", Version: "v1"}})
			mapper.Add(schema.GroupVersionKind{Group: "stackgres.io", Version: "v1", Kind: "SGCluster"}, meta.RESTScopeNamespace)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			calls := 0
			client.PrependReactor("patch", "sgclusters", func(action ktesting.Action) (bool, runtime.Object, error) {
				calls++
				options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
				if !reflect.DeepEqual(options.DryRun, []string{metav1.DryRunAll}) || options.FieldManager != "justcd/app" {
					t.Fatalf("unsafe planning options: %+v", options)
				}
				request := &unstructured.Unstructured{}
				if err := request.UnmarshalJSON(action.(ktesting.PatchAction).GetPatch()); err != nil {
					t.Fatal(err)
				}
				if request.GetResourceVersion() != "12" {
					t.Fatal("lost live precondition")
				}
				scripts, _, _ := unstructured.NestedSlice(request.Object, "spec", "managedSql", "scripts")
				for _, value := range scripts {
					if value.(map[string]interface{})["sgScript"] == "db-default" {
						t.Fatal("operator default must not become part of the apply body")
					}
				}
				if tc.conflict && calls == 1 {
					return true, nil, apierrors.NewApplyConflict([]metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict, Message: `conflict with "stackgres"`, Field: ".spec.managedSql.scripts"}}, "conflict")
				}
				if !tc.conflict && options.Force != nil {
					t.Fatal("ordinary sync must not force ownership")
				}
				if tc.conflict && (options.Force == nil || !*options.Force) {
					t.Fatal("takeover validation must force only the dry run")
				}
				result := &unstructured.Unstructured{}
				if err := result.UnmarshalJSON(manifest(tc.predictedScripts)); err != nil {
					t.Fatal(err)
				}
				result.SetUID("uid")
				result.SetResourceVersion("13")
				result.SetGeneration(4)
				result.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "stackgres"}})
				result.Object["status"] = map[string]interface{}{"ready": true}
				if tc.ignored {
					result.Object["spec"].(map[string]interface{})["instances"] = int64(3)
				}
				if tc.invalid {
					result.SetName("other")
				}
				return true, result, nil
			})
			clients := &kube.Clients{Dynamic: client, Mapper: mapper}
			input := planInput{Application: store.Application{ID: "app", SyncPolicy: "auto-safe"}, Mapper: clients, NamespaceClients: map[string]*kube.Clients{"team": clients}}
			if tc.ignored {
				input.IgnoreRules = []store.ApplicationIgnoreRule{{IgnoreRule: core.IgnoreRule{Identity: id, Path: "/spec/instances"}}}
			}
			err = (&Service{}).reconcileUpdatePlan(context.Background(), input, &plan, []core.Resource{desired}, []core.Resource{live})
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid response was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Changes) != tc.wantChanges {
				t.Fatalf("got %d updates, want %d: %+v", len(plan.Changes), tc.wantChanges, plan.Changes)
			}
			if string(desired.Manifest) != original {
				t.Fatal("dry run rewrote the Git manifest")
			}
			if tc.conflict {
				if calls != 2 || !plan.Changes[0].Takeover {
					t.Fatal("ownership transfer was hidden")
				}
			} else if calls != 1 {
				t.Fatalf("unexpected dry-run count: %d", calls)
			}
			if tc.wantChanges > 0 && !tc.conflict {
				change := plan.Changes[0]
				if !reflect.DeepEqual(change.ChangedPaths, []string{"/spec/managedSql/scripts"}) {
					t.Fatalf("wrong changed paths: %v", change.ChangedPaths)
				}
				var before, after, wantBefore, wantAfter interface{}
				for _, pair := range []struct {
					data   []byte
					target *interface{}
				}{
					{change.Before, &before}, {change.After, &after}, {manifest(liveScripts), &wantBefore}, {manifest(tc.predictedScripts), &wantAfter},
				} {
					if err := json.Unmarshal(pair.data, pair.target); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(before, wantBefore) || !reflect.DeepEqual(after, wantAfter) {
					t.Fatalf("review diff does not show server result: before=%s after=%s", change.Before, change.After)
				}
			}
		})
	}
}
