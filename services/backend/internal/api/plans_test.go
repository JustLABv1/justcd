package api

import (
	"encoding/json"
	"strings"
	"testing"
)

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
