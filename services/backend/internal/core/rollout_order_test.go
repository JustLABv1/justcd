package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func rolloutChange(apiVersion, kind, namespace, name, manifest string) Change {
	return Change{Kind: Create, Identity: Identity{ClusterID: "cluster", APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name, ClusterScoped: namespace == ""}, After: json.RawMessage(manifest)}
}

func orderedRollout(t *testing.T, changes ...Change) []Change {
	t.Helper()
	ordered, err := OrderChanges(changes)
	if err != nil {
		t.Fatal(err)
	}
	return ordered
}

func beforeRollout(t *testing.T, changes []Change, prerequisite, dependent Change) {
	t.Helper()
	positions := map[string]int{}
	for i, change := range changes {
		positions[change.Identity.Key()] = i
	}
	a, aok := positions[prerequisite.Identity.Key()]
	b, bok := positions[dependent.Identity.Key()]
	if !aok || !bok || a >= b {
		t.Fatalf("%s/%s must precede %s/%s: %+v", prerequisite.Identity.Kind, prerequisite.Identity.Name, dependent.Identity.Kind, dependent.Identity.Name, changes)
	}
}

func TestRolloutServiceAccountBeforeWorkload(t *testing.T) {
	sa := rolloutChange("v1", "ServiceAccount", "apps", "runner", `{}`)
	for _, fixture := range []struct{ api, kind, spec string }{
		{"v1", "Pod", `{"spec":{"serviceAccountName":"runner"}}`},
		{"v1", "ReplicationController", `{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}`},
		{"apps/v1", "Deployment", `{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}`},
		{"apps/v1", "StatefulSet", `{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}`},
		{"apps/v1", "DaemonSet", `{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}`},
		{"apps/v1", "ReplicaSet", `{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}`},
		{"batch/v1", "Job", `{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}`},
		{"batch/v1", "CronJob", `{"spec":{"jobTemplate":{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}}}`},
	} {
		t.Run(fixture.kind, func(t *testing.T) {
			workload := rolloutChange(fixture.api, fixture.kind, "apps", "api", fixture.spec)
			workload.Kind = Update
			beforeRollout(t, orderedRollout(t, workload, sa), sa, workload)
		})
	}
}

func TestRolloutRBACAndPodPrerequisites(t *testing.T) {
	namespace := rolloutChange("v1", "Namespace", "", "apps", `{}`)
	sa := rolloutChange("v1", "ServiceAccount", "apps", "runner", `{"imagePullSecrets":[{"name":"pull"}]}`)
	role := rolloutChange("rbac.authorization.k8s.io/v1", "Role", "apps", "reader", `{}`)
	binding := rolloutChange("rbac.authorization.k8s.io/v1", "RoleBinding", "apps", "reader", `{"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"Role","name":"reader"},"subjects":[{"kind":"ServiceAccount","name":"runner"}]}`)
	clusterRole := rolloutChange("rbac.authorization.k8s.io/v1", "ClusterRole", "", "reader", `{}`)
	clusterBinding := rolloutChange("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "reader", `{"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"reader"},"subjects":[{"kind":"ServiceAccount","namespace":"apps","name":"runner"}]}`)
	pull := rolloutChange("v1", "Secret", "apps", "pull", `{}`)
	config := rolloutChange("v1", "ConfigMap", "apps", "settings", `{}`)
	secret := rolloutChange("v1", "Secret", "apps", "credentials", `{}`)
	pvc := rolloutChange("v1", "PersistentVolumeClaim", "apps", "data", `{"spec":{"storageClassName":"fast","volumeName":"data"}}`)
	sc := rolloutChange("storage.k8s.io/v1", "StorageClass", "", "fast", `{}`)
	pv := rolloutChange("v1", "PersistentVolume", "", "data", `{}`)
	service := rolloutChange("v1", "Service", "apps", "headless", `{}`)
	workload := rolloutChange("apps/v1", "StatefulSet", "apps", "api", `{"spec":{"serviceName":"headless","template":{"spec":{"serviceAccountName":"runner","imagePullSecrets":[{"name":"pull"}],"containers":[{"name":"api","env":[{"name":"TOKEN","valueFrom":{"secretKeyRef":{"name":"credentials","key":"token"}}}],"envFrom":[{"configMapRef":{"name":"settings"}}]}],"volumes":[{"name":"data","persistentVolumeClaim":{"claimName":"data"}},{"name":"settings","configMap":{"name":"settings"}},{"name":"credentials","projected":{"sources":[{"secret":{"name":"credentials"}}]}}]}}}}`)
	changes := []Change{workload, pvc, config, sa, binding, secret, role, clusterBinding, clusterRole, namespace, pull, sc, pv, service}
	ordered := orderedRollout(t, changes...)
	for _, prerequisite := range changes[1:] {
		beforeRollout(t, ordered, prerequisite, workload)
	}
	beforeRollout(t, ordered, role, binding)
	beforeRollout(t, ordered, sa, binding)
	beforeRollout(t, ordered, clusterRole, clusterBinding)
	beforeRollout(t, ordered, pull, sa)
	beforeRollout(t, ordered, sc, pvc)
	beforeRollout(t, ordered, pv, pvc)
	for _, change := range changes {
		if change.Identity.Namespace == "apps" {
			beforeRollout(t, ordered, namespace, change)
		}
	}
	// The same graph is used in reverse for deletes, after all creates/updates.
	deletes := make([]Change, len(changes))
	for i, change := range changes {
		change.Kind, change.Before, change.After = Delete, change.After, nil
		deletes[i] = change
	}
	removed := orderedRollout(t, deletes...)
	for i := range ordered {
		if removed[len(removed)-1-i].Identity != ordered[i].Identity {
			t.Fatal("deletions did not reverse prerequisite order")
		}
	}
	newSecret := rolloutChange("v1", "Secret", "apps", "new-secret", `{}`)
	mixed := orderedRollout(t, append(deletes, newSecret)...)
	if mixed[0].Identity != newSecret.Identity {
		t.Fatal("delete occurred before an apply")
	}
}

func TestRolloutReferencesOverrideKindPriority(t *testing.T) {
	// This ownership edge forces a higher-priority ConfigMap after a Pod.
	owner := rolloutChange("v1", "Pod", "apps", "owner", `{"spec":{}}`)
	child := rolloutChange("v1", "ConfigMap", "apps", "child", `{"metadata":{"ownerReferences":[{"apiVersion":"v1","kind":"Pod","name":"owner"}]}}`)
	beforeRollout(t, orderedRollout(t, child, owner), owner, child)
	// Edges are scoped to the exact namespace and cluster, not just kind/name.
	other := owner
	other.Identity.Namespace = "another"
	ordered := orderedRollout(t, other, child)
	if ordered[0].Identity != child.Identity {
		t.Fatal("cross-namespace dependency inferred")
	}
	other.Identity.Namespace, other.Identity.ClusterID = "apps", "other-cluster"
	ordered = orderedRollout(t, other, child)
	if ordered[0].Identity != child.Identity {
		t.Fatal("cross-cluster dependency inferred")
	}
}

func TestRolloutDoesNotRequireExternalReferencesOrInferCustomPodSpecs(t *testing.T) {
	// Missing dependencies may already exist in Kubernetes or be managed elsewhere.
	workload := rolloutChange("apps/v1", "Deployment", "apps", "api", `{"spec":{"template":{"spec":{"serviceAccountName":"external","containers":[{"envFrom":[{"secretRef":{"name":"external"}}]}]}}}}`)
	orderedRollout(t, workload)
	custom := rolloutChange("example.com/v1", "Deployment", "apps", "custom", `{"spec":{"template":"not-a-pod"}}`)
	orderedRollout(t, custom)
}

func TestRolloutRejectsCyclesAndInvalidManifestsWithoutLeakingValues(t *testing.T) {
	a := rolloutChange("v1", "ConfigMap", "apps", "a", `{"metadata":{"ownerReferences":[{"apiVersion":"v1","kind":"ConfigMap","name":"b"}]}}`)
	b := rolloutChange("v1", "ConfigMap", "apps", "b", `{"metadata":{"ownerReferences":[{"apiVersion":"v1","kind":"ConfigMap","name":"a"}]}}`)
	if _, err := OrderChanges([]Change{b, a}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle accepted: %v", err)
	}
	for _, raw := range []string{`{"data":{"token":"sensitive-value"},`, `[]`} {
		secret := rolloutChange("v1", "Secret", "apps", "credentials", raw)
		_, err := OrderChanges([]Change{secret})
		if err == nil || strings.Contains(err.Error(), "sensitive-value") {
			t.Fatalf("unsafe diagnostic: %v", err)
		}
	}
	if _, err := OrderChanges([]Change{a, a}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
}

func TestRolloutOrderingIsDeterministicAndDoesNotMutatePlan(t *testing.T) {
	changes := []Change{
		rolloutChange("v1", "ServiceAccount", "apps", "z", `{}`),
		rolloutChange("apps/v1", "Deployment", "apps", "api", `{"spec":{"template":{"spec":{"serviceAccountName":"z"}}}}`),
		rolloutChange("v1", "ServiceAccount", "apps", "a", `{}`),
	}
	plan := Plan{Changes: changes}
	if err := RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(plan)
	ordered := orderedRollout(t, plan.Changes...)
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(ordered, orderedRollout(t, changes[2], changes[1], changes[0])) {
			t.Fatal("input order changed execution order")
		}
	}
	after, _ := json.Marshal(plan)
	if string(before) != string(after) {
		t.Fatal("reviewed plan or digest mutated")
	}
}
