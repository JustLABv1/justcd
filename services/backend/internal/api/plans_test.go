package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestPlanViewExposesDesiredIdentitiesWithoutManifest(t *testing.T) {
	identity := core.Identity{APIVersion: "v1", Kind: "Secret", Namespace: "demo", Name: "credentials"}
	record := store.PlanRecord{
		Desired: []core.Resource{{Identity: identity, Manifest: json.RawMessage(`{"kind":"Secret","stringData":{"password":"do-not-expose"}}`)}},
	}
	view := toPlanView(record)
	if len(view.Resources) != 1 || view.Resources[0] != identity {
		t.Fatalf("desired resource identity missing from plan view: %+v", view.Resources)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do-not-expose") || strings.Contains(string(encoded), "stringData") {
		t.Fatalf("desired manifest leaked through resource overview: %s", encoded)
	}
}

func TestRollbackPlanViewPreservesTargetProvenanceAndRedactsSecrets(t *testing.T) {
	identity := core.Identity{APIVersion: "v1", Kind: "Secret", Namespace: "demo", Name: "credentials"}
	record := store.PlanRecord{Plan: core.Plan{Rollback: &core.RollbackTarget{Kind: "pre_operation", ID: "snapshot-id", OperationID: "failed-op", Revision: "commit-1", ResourceCount: 1}, Changes: []core.Change{{Kind: core.Update, Identity: identity, Before: json.RawMessage(`{"kind":"Secret","data":{"token":"before-secret"}}`), After: json.RawMessage(`{"kind":"Secret","data":{"token":"after-secret"}}`)}}}}
	view := toPlanView(record)
	if view.Plan.Rollback == nil || view.Plan.Rollback.Kind != "pre_operation" || view.Plan.Rollback.OperationID != "failed-op" {
		t.Fatalf("rollback provenance missing from the plan view: %+v", view.Plan.Rollback)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"before-secret", "after-secret"} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("rollback plan response leaked secret data: %s", encoded)
		}
	}
}

func TestRedactManifestMasksSecretMaterial(t *testing.T) {
	manifest := json.RawMessage(`{"apiVersion":"v1","kind":"Secret","data":{"password":"c2VjcmV0","config":"dG9rZW4="},"stringData":{"api-key":"plaintext"},"metadata":{"name":"example"}}`)
	redacted := string(redactManifest(manifest))
	for _, secret := range []string{"c2VjcmV0", "dG9rZW4=", "plaintext"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret value leaked from plan response: %s", redacted)
		}
	}
	if !strings.Contains(redacted, "example") {
		t.Fatalf("non-secret resource metadata should remain visible: %s", redacted)
	}
}

func TestPlanViewPreservesChangedPathsWhenRedactionMakesManifestsIdentical(t *testing.T) {
	for _, tc := range []struct {
		name, before, after, path string
	}{
		{"Secret data", `{"kind":"Secret","data":{"admin-password":"old"}}`, `{"kind":"Secret","data":{"admin-password":"new"}}`, "/data/admin-password"},
		{"Deployment checksum", `{"kind":"Deployment","spec":{"template":{"metadata":{"annotations":{"checksum/secret":"old"}}}}}`, `{"kind":"Deployment","spec":{"template":{"metadata":{"annotations":{"checksum/secret":"new"}}}}}`, "/spec/template/metadata/annotations/checksum~1secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := toPlanView(store.PlanRecord{Plan: core.Plan{Changes: []core.Change{{Kind: core.Update, Before: json.RawMessage(tc.before), After: json.RawMessage(tc.after), ChangedPaths: []string{tc.path}}}}})
			change := view.Plan.Changes[0]
			if string(change.Before) != string(change.After) || !strings.Contains(string(change.Before), "[redacted]") {
				t.Fatal("expected identical redacted review manifests")
			}
			if len(change.ChangedPaths) != 1 || change.ChangedPaths[0] != tc.path {
				t.Fatalf("change evidence lost during redaction: %+v", change)
			}
		})
	}
}

func TestRedactManifestMasksSensitiveKeysInOtherKinds(t *testing.T) {
	manifest := json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","data":{"ordinary":"value"},"spec":{"clientToken":"sensitive-value","replicas":2}}`)
	redacted := string(redactManifest(manifest))
	if !strings.Contains(redacted, `"ordinary":"value"`) {
		t.Fatalf("ordinary ConfigMap values should remain available for review: %s", redacted)
	}
	for _, secret := range []string{"sensitive-value"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("sensitive value leaked from plan response: %s", redacted)
		}
	}
	if !strings.Contains(redacted, "2") {
		t.Fatalf("ordinary non-secret fields should remain visible: %s", redacted)
	}
}

func TestPlanViewPreservesReviewedStatefulSetRecreation(t *testing.T) {
	change := core.Change{Kind: core.Update, Identity: core.Identity{APIVersion: "apps/v1", Kind: "StatefulSet", Namespace: "team", Name: "db"}, RecreationRequired: true, Recreation: &core.StatefulSetRecreation{PropagationPolicy: "Foreground", Dependencies: []core.RecreationDependency{{Kind: "PersistentVolumeClaim", Name: "data-db-0", UID: "pvc-uid"}}}}
	view := toPlanView(store.PlanRecord{Plan: core.Plan{Changes: []core.Change{change}}})
	got := view.Plan.Changes[0]
	if !got.RecreationRequired || got.Recreation == nil || got.Recreation.PropagationPolicy != "Foreground" || got.Recreation.Dependencies[0].UID != "pvc-uid" {
		t.Fatalf("review omitted destructive operation evidence: %+v", got)
	}
}
