package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func manifestResource(t *testing.T, manifest map[string]any) Resource {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := manifest["metadata"].(map[string]any)
	namespace, _ := meta["namespace"].(string)
	name, _ := meta["name"].(string)
	return Resource{Identity: Identity{APIVersion: manifest["apiVersion"].(string), Kind: manifest["kind"].(string), Namespace: namespace, Name: name}, Manifest: raw}
}

func deployment(labels, selector map[string]any, extraSpec map[string]any) map[string]any {
	spec := map[string]any{
		"selector": map[string]any{"matchLabels": selector},
		"template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": map[string]any{"containers": []any{map[string]any{"name": "web", "image": "nginx"}}}},
	}
	for k, v := range extraSpec {
		spec[k] = v
	}
	return map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "web", "namespace": "dev"}, "spec": spec}
}

func schemaRules(t *testing.T, r Resource) []string {
	t.Helper()
	evaluation, err := (BuiltinPolicyEngine{}).Evaluate(PolicyConfig{DisabledRules: []string{"mutable-image-tags", "missing-resource-limits"}}, []Resource{r})
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, result := range evaluation.Results {
		rules = append(rules, result.Rule+": "+result.Evidence)
	}
	return rules
}

func TestSchemaChecksAcceptValidWorkloadsAndCustomResources(t *testing.T) {
	good := manifestResource(t, deployment(map[string]any{"app": "web"}, map[string]any{"app": "web"}, nil))
	if rules := schemaRules(t, good); len(rules) != 0 {
		t.Fatalf("valid deployment flagged: %v", rules)
	}
	crd := manifestResource(t, map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"name": "Anything_Goes"}, "spec": map[string]any{"whatever": true}})
	if rules := schemaRules(t, crd); len(rules) != 0 {
		t.Fatalf("custom resource flagged: %v", rules)
	}
}

func TestSchemaChecksFindProblems(t *testing.T) {
	for name, tc := range map[string]struct {
		manifest map[string]any
		want     string
	}{
		"unknown field":     {deployment(map[string]any{"app": "web"}, map[string]any{"app": "web"}, map[string]any{"replcas": 2}), "invalid-schema: unknown field"},
		"mistyped field":    {deployment(map[string]any{"app": "web"}, map[string]any{"app": "web"}, map[string]any{"replicas": "two"}), "invalid-schema: field"},
		"selector mismatch": {deployment(map[string]any{"app": "web"}, map[string]any{"app": "api"}, nil), "selector-mismatch"},
		"bad name":          {map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "Bad_Name", "namespace": "dev"}}, "invalid-metadata"},
		"bad label":         {map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "ok", "labels": map[string]any{"team": "has space"}}}, "invalid-metadata"},
	} {
		t.Run(name, func(t *testing.T) {
			rules := schemaRules(t, manifestResource(t, tc.manifest))
			if len(rules) == 0 || !strings.Contains(strings.Join(rules, "\n"), tc.want) {
				t.Fatalf("want %q in %v", tc.want, rules)
			}
		})
	}
}

func TestSchemaEvidenceNeverEchoesValues(t *testing.T) {
	secret := manifestResource(t, map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "creds"}, "stringData": "hunter2-do-not-log"})
	raw, _ := json.Marshal(schemaRules(t, secret))
	if strings.Contains(string(raw), "hunter2") {
		t.Fatalf("value leaked: %s", raw)
	}
}
