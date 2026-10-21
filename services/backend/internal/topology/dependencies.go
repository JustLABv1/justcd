package topology

import (
	"github.com/justlab/justcd/services/backend/internal/core"
)

type dependencyReference struct{ Kind, Namespace, Name string }

// dependencyReferences extracts names only, never literal environment values,
// container arguments, images, tokens, or mounted file contents.
func dependencyReferences(id core.Identity, object map[string]any) []dependencyReference {
	refs := []dependencyReference{}
	add := func(kind, namespace, name string) {
		if name != "" {
			refs = append(refs, dependencyReference{kind, namespace, name})
		}
	}
	if spec := podSpec(object, id.Kind); spec != nil {
		account := nestedString(spec, "serviceAccountName")
		if account == "" {
			account = nestedString(spec, "serviceAccount")
		}
		if account == "" {
			account = "default"
		}
		add("ServiceAccount", id.Namespace, account)
		for _, raw := range nestedSlice(spec, "imagePullSecrets") {
			if ref, ok := raw.(map[string]any); ok {
				add("Secret", id.Namespace, nestedString(ref, "name"))
			}
		}
		for _, raw := range nestedSlice(spec, "volumes") {
			volume, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			add("ConfigMap", id.Namespace, nestedString(volume, "configMap", "name"))
			add("Secret", id.Namespace, nestedString(volume, "secret", "secretName"))
			add("PersistentVolumeClaim", id.Namespace, nestedString(volume, "persistentVolumeClaim", "claimName"))
			for _, raw := range nestedSlice(volume, "projected", "sources") {
				if source, ok := raw.(map[string]any); ok {
					add("ConfigMap", id.Namespace, nestedString(source, "configMap", "name"))
					add("Secret", id.Namespace, nestedString(source, "secret", "name"))
				}
			}
		}
		for _, kind := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for _, raw := range nestedSlice(spec, kind) {
				container, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				for _, raw := range nestedSlice(container, "envFrom") {
					if source, ok := raw.(map[string]any); ok {
						add("ConfigMap", id.Namespace, nestedString(source, "configMapRef", "name"))
						add("Secret", id.Namespace, nestedString(source, "secretRef", "name"))
					}
				}
				for _, raw := range nestedSlice(container, "env") {
					if env, ok := raw.(map[string]any); ok {
						add("ConfigMap", id.Namespace, nestedString(env, "valueFrom", "configMapKeyRef", "name"))
						add("Secret", id.Namespace, nestedString(env, "valueFrom", "secretKeyRef", "name"))
					}
				}
			}
		}
	}
	if id.Kind == "RoleBinding" || id.Kind == "ClusterRoleBinding" {
		namespace := id.Namespace
		if nestedString(object, "roleRef", "kind") == "ClusterRole" {
			namespace = ""
		}
		add(nestedString(object, "roleRef", "kind"), namespace, nestedString(object, "roleRef", "name"))
		for _, raw := range nestedSlice(object, "subjects") {
			if subject, ok := raw.(map[string]any); ok && nestedString(subject, "kind") == "ServiceAccount" {
				namespace := nestedString(subject, "namespace")
				if namespace == "" {
					namespace = id.Namespace
				}
				add("ServiceAccount", namespace, nestedString(subject, "name"))
			}
		}
	}
	return refs
}

func observationReferences(kind string, object map[string]any) []any {
	refs := []any{}
	for _, ref := range dependencyReferences(core.Identity{Kind: kind, Namespace: nestedString(object, "metadata", "namespace")}, object) {
		refs = append(refs, map[string]any{"kind": ref.Kind, "namespace": ref.Namespace, "name": ref.Name})
	}
	return refs
}

func storedReferences(id core.Identity, snapshot map[string]any) []dependencyReference {
	refs := []dependencyReference{}
	for _, raw := range nestedSlice(snapshot, "references") {
		if entry, ok := raw.(map[string]any); ok {
			namespace := nestedString(entry, "namespace")
			if namespace == "" && nestedString(entry, "kind") != "ClusterRole" {
				namespace = id.Namespace
			}
			refs = append(refs, dependencyReference{nestedString(entry, "kind"), namespace, nestedString(entry, "name")})
		}
	}
	return refs
}
