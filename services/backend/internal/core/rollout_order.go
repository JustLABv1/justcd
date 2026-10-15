package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OrderChanges computes execution order without changing the reviewed plan or
// its digest. Applies precede deletes; deletions reverse the dependency graph.
// Only references to resources in the same phase of this plan create edges.
func OrderChanges(changes []Change) ([]Change, error) {
	var apply, remove []Change
	for _, change := range changes {
		if change.Kind == Delete {
			remove = append(remove, change)
		} else {
			apply = append(apply, change)
		}
	}
	apply, err := orderChangePhase(apply, false)
	if err != nil {
		return nil, err
	}
	remove, err = orderChangePhase(remove, true)
	if err != nil {
		return nil, err
	}
	return append(apply, remove...), nil
}

type rolloutObject struct {
	Metadata         metav1.ObjectMeta                        `json:"metadata"`
	Spec             json.RawMessage                          `json:"spec"`
	RoleRef          struct{ APIGroup, Kind, Name string }    `json:"roleRef"`
	Subjects         []struct{ Kind, Name, Namespace string } `json:"subjects"`
	ImagePullSecrets []corev1.LocalObjectReference            `json:"imagePullSecrets"`
}

func orderChangePhase(changes []Change, deleting bool) ([]Change, error) {
	// Sorting first makes both graph traversal and cycle diagnostics reproducible.
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i].Identity, changes[j].Identity
		if rolloutPriority(a) != rolloutPriority(b) {
			return rolloutPriority(a) < rolloutPriority(b)
		}
		return a.Key() < b.Key()
	})
	objects := make([]rolloutObject, len(changes))
	index := make(map[string]int, len(changes))
	for i, change := range changes {
		if _, exists := index[change.Identity.Key()]; exists {
			return nil, fmt.Errorf("duplicate rollout resource %s %s/%s", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name)
		}
		index[change.Identity.Key()] = i
		raw := change.After
		if deleting {
			raw = change.Before
		}
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &objects[i]); err != nil {
				return nil, fmt.Errorf("cannot read rollout dependencies for %s %s/%s", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name)
			}
		}
	}
	deps := make([]map[int]bool, len(changes))
	for i, change := range changes {
		deps[i] = map[int]bool{}
		id, obj := change.Identity, objects[i]
		ref := func(apiVersion, kind, namespace, name string) {
			if name == "" {
				return
			}
			target := Identity{ClusterID: id.ClusterID, APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name}
			if j, ok := index[target.Key()]; ok && i != j {
				deps[i][j] = true
			}
		}
		if id.Namespace != "" {
			ref("v1", "Namespace", "", id.Namespace)
		}
		for _, owner := range obj.Metadata.OwnerReferences {
			ref(owner.APIVersion, owner.Kind, id.Namespace, owner.Name)
			ref(owner.APIVersion, owner.Kind, "", owner.Name)
		}
		if id.APIVersion == "rbac.authorization.k8s.io/v1" && (id.Kind == "RoleBinding" || id.Kind == "ClusterRoleBinding") {
			ns := id.Namespace
			if obj.RoleRef.Kind == "ClusterRole" {
				ns = ""
			}
			ref(obj.RoleRef.APIGroup+"/v1", obj.RoleRef.Kind, ns, obj.RoleRef.Name)
			for _, subject := range obj.Subjects {
				if subject.Kind == "ServiceAccount" {
					ns := subject.Namespace
					if ns == "" {
						ns = id.Namespace
					}
					ref("v1", "ServiceAccount", ns, subject.Name)
				}
			}
		}
		if id.APIVersion == "v1" && id.Kind == "ServiceAccount" {
			for _, secret := range obj.ImagePullSecrets {
				ref("v1", "Secret", id.Namespace, secret.Name)
			}
		}
		if id.APIVersion == "v1" && id.Kind == "Secret" {
			ref("v1", "ServiceAccount", id.Namespace, obj.Metadata.Annotations["kubernetes.io/service-account.name"])
		}
		if id.APIVersion == "v1" && id.Kind == "PersistentVolumeClaim" {
			var spec corev1.PersistentVolumeClaimSpec
			if len(obj.Spec) > 0 {
				if err := json.Unmarshal(obj.Spec, &spec); err != nil {
					return nil, fmt.Errorf("cannot read PVC dependencies for %s/%s", id.Namespace, id.Name)
				}
			}
			if spec.StorageClassName != nil {
				ref("storage.k8s.io/v1", "StorageClass", "", *spec.StorageClassName)
			}
			ref("v1", "PersistentVolume", "", spec.VolumeName)
		}
		pod, err := rolloutPodSpec(id, obj.Spec)
		if err != nil {
			return nil, err
		}
		if pod == nil {
			continue
		}
		sa := pod.ServiceAccountName
		if sa == "" {
			sa = pod.DeprecatedServiceAccount
		}
		if sa == "" {
			sa = "default"
		}
		ref("v1", "ServiceAccount", id.Namespace, sa)
		// Binding application must precede pod creation as well as the account.
		for j, binding := range changes {
			bid := binding.Identity
			if bid.ClusterID != id.ClusterID || bid.APIVersion != "rbac.authorization.k8s.io/v1" || (bid.Kind != "RoleBinding" && bid.Kind != "ClusterRoleBinding") {
				continue
			}
			for _, subject := range objects[j].Subjects {
				ns := subject.Namespace
				if ns == "" {
					ns = bid.Namespace
				}
				if subject.Kind == "ServiceAccount" && subject.Name == sa && ns == id.Namespace {
					deps[i][j] = true
				}
			}
		}
		localRef := func(kind, name string) { ref("v1", kind, id.Namespace, name) }
		for _, secret := range pod.ImagePullSecrets {
			localRef("Secret", secret.Name)
		}
		containerRefs := func(env []corev1.EnvVar, from []corev1.EnvFromSource) {
			for _, entry := range env {
				if entry.ValueFrom == nil {
					continue
				}
				if r := entry.ValueFrom.SecretKeyRef; r != nil {
					localRef("Secret", r.Name)
				}
				if r := entry.ValueFrom.ConfigMapKeyRef; r != nil {
					localRef("ConfigMap", r.Name)
				}
			}
			for _, entry := range from {
				if entry.SecretRef != nil {
					localRef("Secret", entry.SecretRef.Name)
				}
				if entry.ConfigMapRef != nil {
					localRef("ConfigMap", entry.ConfigMapRef.Name)
				}
			}
		}
		for _, c := range pod.Containers {
			containerRefs(c.Env, c.EnvFrom)
		}
		for _, c := range pod.InitContainers {
			containerRefs(c.Env, c.EnvFrom)
		}
		for _, c := range pod.EphemeralContainers {
			containerRefs(c.Env, c.EnvFrom)
		}
		for _, volume := range pod.Volumes {
			if volume.Secret != nil {
				localRef("Secret", volume.Secret.SecretName)
			}
			if volume.ConfigMap != nil {
				localRef("ConfigMap", volume.ConfigMap.Name)
			}
			if volume.PersistentVolumeClaim != nil {
				localRef("PersistentVolumeClaim", volume.PersistentVolumeClaim.ClaimName)
			}
			if volume.CSI != nil && volume.CSI.NodePublishSecretRef != nil {
				localRef("Secret", volume.CSI.NodePublishSecretRef.Name)
			}
			if volume.Projected != nil {
				for _, source := range volume.Projected.Sources {
					if source.Secret != nil {
						localRef("Secret", source.Secret.Name)
					}
					if source.ConfigMap != nil {
						localRef("ConfigMap", source.ConfigMap.Name)
					}
				}
			}
		}
		if id.APIVersion == "apps/v1" && id.Kind == "StatefulSet" {
			var spec struct {
				ServiceName string `json:"serviceName"`
			}
			_ = json.Unmarshal(obj.Spec, &spec) // Already decoded by rolloutPodSpec.
			localRef("Service", spec.ServiceName)
		}
	}
	state := make([]uint8, len(changes))
	ordered := make([]Change, 0, len(changes))
	var visit func(int) error
	visit = func(i int) error {
		if state[i] == 2 {
			return nil
		}
		if state[i] == 1 {
			id := changes[i].Identity
			return fmt.Errorf("rollout dependency cycle involving %s %s/%s", id.Kind, id.Namespace, id.Name)
		}
		state[i] = 1
		var prerequisites []int
		for j := range deps[i] {
			prerequisites = append(prerequisites, j)
		}
		sort.Ints(prerequisites)
		for _, j := range prerequisites {
			if err := visit(j); err != nil {
				return err
			}
		}
		state[i] = 2
		ordered = append(ordered, changes[i])
		return nil
	}
	for i := range changes {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	if deleting {
		for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
			ordered[i], ordered[j] = ordered[j], ordered[i]
		}
	}
	return ordered, nil
}

// Kind priority gives deterministic, conservative ordering for resources with
// no explicit reference. It does not infer dependencies in arbitrary CR specs.
func rolloutPriority(id Identity) int {
	switch id.APIVersion + "/" + id.Kind {
	case "v1/Namespace":
		return 0
	case "apiextensions.k8s.io/v1/CustomResourceDefinition":
		return 1
	case "storage.k8s.io/v1/StorageClass", "v1/PersistentVolume":
		return 2
	case "v1/ServiceAccount":
		return 3
	case "v1/Secret", "v1/ConfigMap":
		return 4
	case "rbac.authorization.k8s.io/v1/Role", "rbac.authorization.k8s.io/v1/ClusterRole":
		return 5
	case "rbac.authorization.k8s.io/v1/RoleBinding", "rbac.authorization.k8s.io/v1/ClusterRoleBinding":
		return 6
	case "v1/PersistentVolumeClaim", "v1/Service":
		return 7
	default:
		return 10
	}
}

func rolloutPodSpec(id Identity, raw json.RawMessage) (*corev1.PodSpec, error) {
	path := ""
	switch id.APIVersion + "/" + id.Kind {
	case "v1/Pod":
		path = "spec"
	case "v1/ReplicationController", "apps/v1/Deployment", "apps/v1/StatefulSet", "apps/v1/DaemonSet", "apps/v1/ReplicaSet", "batch/v1/Job":
		path = "spec.template.spec"
	case "batch/v1/CronJob":
		path = "spec.jobTemplate.spec.template.spec"
	default:
		return nil, nil
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	for _, field := range strings.Split(path, ".")[1:] {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, fmt.Errorf("cannot read pod dependencies for %s %s/%s", id.Kind, id.Namespace, id.Name)
		}
		raw = object[field]
		if len(raw) == 0 {
			return nil, nil
		}
	}
	var pod corev1.PodSpec
	if err := json.Unmarshal(raw, &pod); err != nil {
		return nil, fmt.Errorf("cannot read pod dependencies for %s %s/%s", id.Kind, id.Namespace, id.Name)
	}
	return &pod, nil
}
