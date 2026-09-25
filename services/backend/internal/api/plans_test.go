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
