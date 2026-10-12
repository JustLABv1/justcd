package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func policyFixture(kind, apiVersion, namespace, name string, spec any) Resource {
	raw, _ := json.Marshal(map[string]any{"spec": spec})
	return Resource{Identity: Identity{Kind: kind, APIVersion: apiVersion, Namespace: namespace, Name: name, ClusterScoped: namespace == ""}, Manifest: raw}
}
func safeContainer() map[string]any {
	return map[string]any{"image": "nginx@sha256:" + strings.Repeat("a", 64), "resources": map[string]any{"limits": map[string]any{"cpu": "100m", "memory": "64Mi"}}}
}
func TestBuiltinPolicies(t *testing.T) {
	cases := []struct {
		rule      string
		bad, good Resource
	}{
		{"privileged-containers", policyFixture("Pod", "v1", "dev", "bad", map[string]any{"containers": []any{map[string]any{"securityContext": map[string]any{"privileged": true}}}}), policyFixture("Pod", "v1", "dev", "good", map[string]any{"containers": []any{safeContainer()}})},
		{"mutable-image-tags", policyFixture("Pod", "v1", "dev", "bad", map[string]any{"containers": []any{map[string]any{"image": "nginx:1.27"}}}), policyFixture("Pod", "v1", "dev", "good", map[string]any{"containers": []any{safeContainer()}})},
		{"missing-resource-limits", policyFixture("Pod", "v1", "dev", "bad", map[string]any{"containers": []any{map[string]any{"resources": map[string]any{"limits": map[string]any{"cpu": "0", "memory": "-1"}}}}}), policyFixture("Pod", "v1", "dev", "good", map[string]any{"containers": []any{safeContainer()}})},
		{"public-services", policyFixture("Service", "v1", "dev", "bad", map[string]any{"type": "LoadBalancer"}), policyFixture("Service", "v1", "dev", "good", map[string]any{"type": "ClusterIP"})},
		{"forbidden-namespaces", policyFixture("Namespace", "v1", "", "kube-system", map[string]any{}), policyFixture("Namespace", "v1", "", "dev", map[string]any{})},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			config := PolicyConfig{Mode: "block", ForbiddenNamespaces: []string{"kube-system"}}
			for _, rule := range BuiltinPolicyRules {
				if rule != tc.rule {
					config.DisabledRules = append(config.DisabledRules, rule)
				}
			}
			bad, err := (BuiltinPolicyEngine{}).Evaluate(config, []Resource{tc.bad})
			if err != nil || len(bad.Results) == 0 {
				t.Fatalf("missing violation: %+v %v", bad, err)
			}
			for _, finding := range bad.Results {
				if finding.Rule != tc.rule || finding.Resource != tc.bad.Identity || finding.Evidence == "" || finding.Remediation == "" {
					t.Fatalf("incomplete evidence: %+v", finding)
				}
			}
			good, err := (BuiltinPolicyEngine{}).Evaluate(config, []Resource{tc.good})
			if err != nil || len(good.Results) != 0 {
				t.Fatalf("false positive: %+v %v", good, err)
			}
		})
	}
}
func TestPolicyWorkloadAndContainerCoverage(t *testing.T) {
	for _, kind := range []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job", "CronJob"} {
		for _, group := range []string{"containers", "initContainers", "ephemeralContainers"} {
			t.Run(kind+"/"+group, func(t *testing.T) {
				pod := map[string]any{group: []any{map[string]any{"image": "nginx", "securityContext": map[string]any{"privileged": true}}}, "hostPID": true}
				spec := pod
				version := "v1"
				if kind != "Pod" {
					spec = map[string]any{"template": map[string]any{"spec": pod}}
					version = "apps/v1"
				}
				if kind == "ReplicationController" {
					version = "v1"
				}
				if kind == "Job" || kind == "CronJob" {
					version = "batch/v1"
				}
				if kind == "CronJob" {
					spec = map[string]any{"jobTemplate": map[string]any{"spec": spec}}
				}
				evaluation, err := (BuiltinPolicyEngine{}).Evaluate(PolicyConfig{}, []Resource{policyFixture(kind, version, "dev", "test", spec)})
				expected := 5
				if group == "ephemeralContainers" {
					expected = 3
				}
				if err != nil || len(evaluation.Results) != expected {
					t.Fatalf("got %+v %v", evaluation, err)
				}
			})
		}
	}
	for _, spec := range []map[string]any{{"type": "NodePort"}, {"externalIPs": []any{"1.2.3.4"}}} {
		result, err := (BuiltinPolicyEngine{}).Evaluate(PolicyConfig{}, []Resource{policyFixture("Service", "v1", "dev", "service", spec)})
		if err != nil || len(result.Results) != 1 {
			t.Fatalf("public service not detected: %+v %v", result, err)
		}
	}
}
func TestPolicyEnforcementAndDigest(t *testing.T) {
	desired := []Resource{policyFixture("Service", "v1", "dev", "service", map[string]any{"type": "NodePort"})}
	for _, mode := range []string{"audit", "warn", "block"} {
		evaluation, err := (BuiltinPolicyEngine{}).Evaluate(PolicyConfig{Mode: mode}, desired)
		if err != nil {
			t.Fatal(err)
		}
		plan := Plan{PolicyEvaluation: &evaluation}
		if err := RefreshDigest(&plan); err != nil {
			t.Fatal(err)
		}
		before := plan.Digest
		err = AuthorizeApplyMany(plan, []DeletionApproval{{ActorID: "owner", PlanDigest: plan.Digest, ExpiresAt: time.Now().Add(time.Hour)}}, time.Now())
		if (err != nil) != (mode == "block") {
			t.Fatalf("mode %s authorization: %v", mode, err)
		}
		plan.PolicyEvaluation.Results[0].Remediation = "different"
		if err := RefreshDigest(&plan); err != nil {
			t.Fatal(err)
		}
		if before == plan.Digest {
			t.Fatal("results not digest-bound")
		}
	}
	first, _ := PolicyConfigDigest(PolicyConfig{ForbiddenNamespaces: []string{"prod", "dev", "prod"}})
	second, _ := PolicyConfigDigest(PolicyConfig{Mode: "audit", ForbiddenNamespaces: []string{"dev", "prod"}})
	if first != second {
		t.Fatal("equivalent configs differ")
	}
	changed, _ := PolicyConfigDigest(PolicyConfig{Mode: "block", ForbiddenNamespaces: []string{"dev", "prod"}})
	if changed == first {
		t.Fatal("mode not digest-bound")
	}
}
func TestPolicyInputIsolationAndDeterminism(t *testing.T) {
	resources := []Resource{policyFixture("Pod", "v1", "dev", "z", map[string]any{"containers": []any{map[string]any{"image": "secret-do-not-log", "env": []any{map[string]any{"value": "secret-do-not-log"}}}}}), policyFixture("Service", "v1", "dev", "a", map[string]any{"type": "NodePort"})}
	original, _ := json.Marshal(resources)
	evaluation, err := (BuiltinPolicyEngine{}).Evaluate(PolicyConfig{}, resources)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(resources)
	if string(after) != string(original) {
		t.Fatal("policy mutated input")
	}
	raw, _ := json.Marshal(evaluation)
	if strings.Contains(string(raw), "secret-do-not-log") {
		t.Fatal("sensitive value leaked")
	}
	reversed := []Resource{resources[1], resources[0]}
	other, err := (BuiltinPolicyEngine{}).Evaluate(PolicyConfig{}, reversed)
	if err != nil || !reflect.DeepEqual(evaluation, other) {
		t.Fatal("non-deterministic evaluation")
	}
	for _, raw := range []string{"{", "null", "[]"} {
		_, err := (BuiltinPolicyEngine{}).Evaluate(PolicyConfig{}, []Resource{{Manifest: json.RawMessage(raw)}})
		if err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	for _, config := range []PolicyConfig{{Mode: "allow"}, {DisabledRules: []string{"unknown"}}, {ForbiddenNamespaces: []string{"bad namespace"}}} {
		if _, err := NormalizePolicyConfig(config); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
