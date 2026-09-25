package syncer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/security"
)

func TestRollbackSnapshotPayloadIsEncryptedAndContextBound(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	manifest := json.RawMessage(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"app-secret","namespace":"demo"},"data":{"token":"rollback-secret-value"}}`)
	payload, err := json.Marshal(rollbackSnapshotPayload{
		Version:   1,
		Settings:  core.RollbackSettings{SourceID: "source-1", Revision: "main", ManifestPath: "deploy/app", Renderer: "yaml", ClusterID: "cluster-1", Namespaces: []string{"demo"}},
		Resources: []core.Resource{{Identity: core.Identity{ClusterID: "cluster-1", APIVersion: "v1", Kind: "Secret", Namespace: "demo", Name: "app-secret"}, Fingerprint: "fingerprint", Manifest: manifest}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := security.Encrypt(key, payload, "rollback-snapshot:app-1:snapshot-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ciphertext), "rollback-secret-value") || strings.Contains(string(ciphertext), "app-secret") {
		t.Fatal("snapshot ciphertext contains plaintext manifest data")
	}
	plaintext, err := security.Decrypt(key, ciphertext, "rollback-snapshot:app-1:snapshot-1")
	if err != nil || !strings.Contains(string(plaintext), "rollback-secret-value") {
		t.Fatalf("encrypted snapshot could not be restored: %v", err)
	}
	if _, err := security.Decrypt(key, ciphertext, "rollback-snapshot:other-app:snapshot-1"); err == nil {
		t.Fatal("snapshot ciphertext was accepted for a different application context")
	}
}

func TestRollbackStepBindingCheckDetectsCredentialRotation(t *testing.T) {
	before := []core.Binding{{ClusterID: "cluster-1", Namespace: "apps", CredentialRef: "credential-a"}}
	after := []core.Binding{{ClusterID: "cluster-1", Namespace: "apps", CredentialRef: "credential-b"}}
	if samePlanBindings(before, after) {
		t.Fatal("a changed Kubernetes credential must invalidate the next rollback step")
	}
	if !samePlanBindings(before, []core.Binding{before[0]}) {
		t.Fatal("an unchanged credential binding should remain valid")
	}
}
