package repoconfig

import (
	"strings"
	"testing"
)

func TestDiscoverResourceIgnores(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "apps/justcd.yaml", validDefinition+`  ignoreResources:
    - apiVersion: secrets.hashicorp.com/v1beta1
      kind: VaultStaticSecret
      reason: Managed by Vault
    - apiVersion: v1
      kind: Secret
      name: ntfy-env
      reason: Managed by Vault
    - labelKey: justcd.io/skip
      labelValue: "true"
      reason: Managed externally
    - apiVersion: v1
      kind: Namespace
      name: shop
      clusterScoped: true
      reason: Managed by administrator
`)
	definitions, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	rules := definitions[0].Spec.IgnoreResources
	if len(rules) != 4 || rules[1].Namespace != "shop" || rules[3].Namespace != "" {
		t.Fatalf("incorrect rules: %+v", rules)
	}
	originalHash := definitions[0].Hash
	writeDefinition(t, root, "apps/justcd.yaml", validDefinition)
	definitions, err = Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if definitions[0].Hash == originalHash {
		t.Fatal("ignores did not affect configuration hash")
	}
}

func TestDiscoverRejectsInvalidIgnores(t *testing.T) {
	cases := map[string]string{
		"missing selector":       "- reason: Something external",
		"no reason":              "- apiVersion: v1\n      kind: Secret",
		"short reason":           "- apiVersion: v1\n      kind: Secret\n      reason: why",
		"partial kind":           "- kind: Secret\n      reason: External resource",
		"mixed selector":         "- apiVersion: v1\n      kind: Secret\n      labelKey: skip\n      reason: External resource",
		"wrong namespace":        "- apiVersion: v1\n      kind: Secret\n      name: example\n      namespace: other\n      reason: External resource",
		"scope mismatch":         "- apiVersion: v1\n      kind: Secret\n      name: example\n      namespace: shop\n      clusterScoped: true\n      reason: External resource",
		"namespace without name": "- apiVersion: v1\n      kind: Secret\n      namespace: shop\n      reason: External resource",
		"bad apiVersion":         "- apiVersion: secrets.hashicorp.com/v1/bad\n      kind: Secret\n      reason: External resource",
		"bad label":              "- labelKey: 'bad key'\n      reason: External resource",
		"value without label":    "- labelValue: 'true'\n      reason: External resource",
		"field path":             "- apiVersion: v1\n      kind: Secret\n      name: example\n      path: /data\n      reason: External resource",
		"duplicate":              "- apiVersion: v1\n      kind: Secret\n      reason: External resource\n    - apiVersion: v1\n      kind: Secret\n      reason: Another reason",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeDefinition(t, root, "justcd.yaml", validDefinition+"  ignoreResources:\n    "+entry+"\n")
			if _, err := Discover(root); err == nil {
				t.Fatal("invalid ignore accepted")
			}
		})
	}
	t.Run("too many", func(t *testing.T) {
		root := t.TempDir()
		writeDefinition(t, root, "justcd.yaml", validDefinition+"  ignoreResources:\n"+strings.Repeat("    - apiVersion: v1\n      kind: Secret\n      reason: External resource\n", 101))
		if _, err := Discover(root); err == nil {
			t.Fatal("unbounded ignore list")
		}
	})
}
