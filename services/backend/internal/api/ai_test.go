package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAIManifestExcludesSecretValuesAndArbitrarySpecs(t *testing.T) {
	cases := []string{
		`{"kind":"Secret","data":{"password":"SECRET_VALUE"},"stringData":{"key":"SECRET_VALUE"}}`,
		`{"kind":"ConfigMap","data":{"anything":"SECRET_VALUE"}}`,
		`{"kind":"CustomThing","spec":{"password":"SECRET_VALUE","image":"SECRET_VALUE"}}`,
		`{"kind":"Deployment","metadata":{"annotations":{"anything":"SECRET_VALUE"}},"spec":{"replicas":3,"template":{"spec":{"containers":[{"image":"nginx:2","env":[{"name":"ORDINARY","value":"SECRET_VALUE"}],"command":["SECRET_VALUE"],"args":["SECRET_VALUE"],"readinessProbe":{"httpGet":{"path":"/ready","httpHeaders":[{"value":"SECRET_VALUE"}]}}}]}}}}`,
	}
	for _, raw := range cases {
		encoded, _ := json.Marshal(aiManifest(json.RawMessage(raw)))
		if strings.Contains(string(encoded), "SECRET_VALUE") {
			t.Fatalf("secret leaked: %s", encoded)
		}
	}
	encoded, _ := json.Marshal(aiManifest(json.RawMessage(cases[3])))
	for _, field := range []string{"nginx:2", "replicas", "/ready"} {
		if !strings.Contains(string(encoded), field) {
			t.Errorf("lost useful field %s", field)
		}
	}
}
func TestAIContextScrubsDiagnosticCredentials(t *testing.T) {
	encoded, _ := json.Marshal(aiScrub(map[string]any{"message": "Authorization: Bearer super-secret-value", "nested": []string{"password=hidden-value"}}))
	for _, secret := range []string{"super-secret-value", "hidden-value"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("credential leaked: %s", encoded)
		}
	}
}
