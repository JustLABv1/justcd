package topology

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Node struct {
	ID              string        `json:"id"`
	Identity        core.Identity `json:"identity"`
	Source          string        `json:"source"`
	UID             string        `json:"uid,omitempty"`
	ResourceVersion string        `json:"resourceVersion,omitempty"`
	Phase           string        `json:"phase,omitempty"`
	Readiness       string        `json:"readiness,omitempty"`
	ObservedAt      *time.Time    `json:"observedAt,omitempty"`
}

type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

type Graph struct {
	PlanID   string   `json:"planId,omitempty"`
	Nodes    []Node   `json:"nodes"`
	Edges    []Edge   `json:"edges"`
	Warnings []string `json:"warnings"`
}

type material struct {
	node   Node
	value  map[string]any
	labels map[string]string
	owners []string
}

func ID(identity core.Identity) string {
	sum := sha256.Sum256([]byte(identity.Key()))
	return hex.EncodeToString(sum[:])
}

func Build(plan *store.PlanRecord, managed []store.ManagedResource, observed []store.ObservedResource) Graph {
	graph := Graph{Nodes: []Node{}, Edges: []Edge{}, Warnings: []string{}}
	items := map[string]*material{}
	if plan != nil {
		graph.PlanID = plan.ID
		for _, resource := range plan.Desired {
			identity := resource.Identity
			items[ID(identity)] = &material{node: Node{ID: ID(identity), Identity: identity, Source: "desired"}, value: parse(resource.Manifest)}
		}
	}
	for _, resource := range managed {
		identity := resource.Identity
		identity.ClusterScoped = identity.Namespace == ""
		key := ID(identity)
		if existing := items[key]; existing != nil {
			existing.node.UID = resource.UID
			existing.node.ResourceVersion = resource.ResourceVersion
			if existing.value == nil {
				existing.value = parse(resource.Manifest)
			}
		} else {
			items[key] = &material{node: Node{ID: key, Identity: identity, Source: "managed", UID: resource.UID, ResourceVersion: resource.ResourceVersion}, value: parse(resource.Manifest)}
		}
	}
	for _, resource := range observed {
		identity := resource.Identity
		key := ID(identity)
		at := resource.ObservedAt
		items[key] = &material{node: Node{ID: key, Identity: identity, Source: resource.Source, UID: resource.UID, ResourceVersion: resource.ResourceVersion, Phase: resource.Phase, Readiness: resource.Readiness, ObservedAt: &at}, labels: resource.Labels, owners: resource.OwnerUIDs}
	}

	byUID := map[string]*material{}
	byName := map[string]*material{}
	for _, item := range items {
		if item.node.UID != "" {
			byUID[item.node.UID] = item
		}
		byName[nameKey(item.node.Identity.Kind, item.node.Identity.Namespace, item.node.Identity.Name)] = item
		if item.labels == nil {
			item.labels = nestedStringMap(item.value, "metadata", "labels")
		}
		if len(item.owners) == 0 {
			for _, ref := range nestedSlice(item.value, "metadata", "ownerReferences") {
				if owner, ok := ref.(map[string]any); ok {
					if uid, ok := owner["uid"].(string); ok {
						item.owners = append(item.owners, uid)
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	add := func(from, to *material, relation string) {
		if from == nil || to == nil || from.node.ID == to.node.ID {
			return
		}
		key := from.node.ID + "/" + to.node.ID + "/" + relation
		if seen[key] {
			return
		}
		seen[key] = true
		graph.Edges = append(graph.Edges, Edge{From: from.node.ID, To: to.node.ID, Relation: relation})
	}
	for _, item := range items {
		id := item.node.Identity
		for _, owner := range item.owners {
			add(byUID[owner], item, "owns")
		}
		switch id.Kind {
		case "Ingress":
			if name := nestedString(item.value, "spec", "defaultBackend", "service", "name"); name != "" {
				add(item, byName[nameKey("Service", id.Namespace, name)], "routes to")
			}
			for _, rule := range nestedSlice(item.value, "spec", "rules") {
				r, ok := rule.(map[string]any)
				if !ok {
					continue
				}
				for _, path := range nestedSlice(r, "http", "paths") {
					p, ok := path.(map[string]any)
					if !ok {
						continue
					}
					if name := nestedString(p, "backend", "service", "name"); name != "" {
						add(item, byName[nameKey("Service", id.Namespace, name)], "routes to")
					}
				}
			}
		case "HTTPRoute":
			for _, rule := range nestedSlice(item.value, "spec", "rules") {
				r, ok := rule.(map[string]any)
				if !ok {
					continue
				}
				for _, backend := range nestedSlice(r, "backendRefs") {
					b, ok := backend.(map[string]any)
					if !ok {
						continue
					}
					if name := nestedString(b, "name"); name != "" {
						add(item, byName[nameKey("Service", id.Namespace, name)], "routes to")
					}
				}
			}
		case "Service":
			selector := nestedStringMap(item.value, "spec", "selector")
			if len(selector) == 0 {
				break
			}
			matchedWorkload := false
			for _, target := range items {
				if target.node.Identity.Namespace != id.Namespace || !isWorkload(target.node.Identity.Kind) {
					continue
				}
				labels := nestedStringMap(target.value, "spec", "template", "metadata", "labels")
				if matches(selector, labels) {
					add(item, target, "selects")
					matchedWorkload = true
				}
			}
			if !matchedWorkload {
				for _, target := range items {
					if target.node.Identity.Kind == "Pod" && target.node.Identity.Namespace == id.Namespace && matches(selector, target.labels) {
						add(item, target, "selects")
					}
				}
			}
		case "HorizontalPodAutoscaler":
			kind := nestedString(item.value, "spec", "scaleTargetRef", "kind")
			name := nestedString(item.value, "spec", "scaleTargetRef", "name")
			add(item, byName[nameKey(kind, id.Namespace, name)], "scales")
		}
		if isWorkload(id.Kind) {
			spec := podSpec(item.value, id.Kind)
			if spec != nil {
				if name := nestedString(spec, "serviceAccountName"); name != "" {
					add(item, byName[nameKey("ServiceAccount", id.Namespace, name)], "uses")
				}
				for _, volume := range nestedSlice(spec, "volumes") {
					v, ok := volume.(map[string]any)
					if !ok {
						continue
					}
					for _, ref := range []struct {
						path []string
						kind string
					}{{[]string{"configMap", "name"}, "ConfigMap"}, {[]string{"secret", "secretName"}, "Secret"}, {[]string{"persistentVolumeClaim", "claimName"}, "PersistentVolumeClaim"}} {
						if name := nestedString(v, ref.path...); name != "" {
							add(item, byName[nameKey(ref.kind, id.Namespace, name)], "uses")
						}
					}
				}
				for _, containerType := range []string{"containers", "initContainers"} {
					for _, container := range nestedSlice(spec, containerType) {
						c, ok := container.(map[string]any)
						if !ok {
							continue
						}
						for _, source := range nestedSlice(c, "envFrom") {
							v, ok := source.(map[string]any)
							if !ok {
								continue
							}
							if name := nestedString(v, "configMapRef", "name"); name != "" {
								add(item, byName[nameKey("ConfigMap", id.Namespace, name)], "uses")
							}
							if name := nestedString(v, "secretRef", "name"); name != "" {
								add(item, byName[nameKey("Secret", id.Namespace, name)], "uses")
							}
						}
					}
				}
			}
		}
	}
	for _, item := range items {
		graph.Nodes = append(graph.Nodes, item.node)
	}
	sortGraph(&graph)
	return graph
}

func parse(raw json.RawMessage) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}
func nameKey(kind, namespace, name string) string {
	return strings.ToLower(kind) + "\x00" + namespace + "\x00" + name
}
func nestedString(value map[string]any, path ...string) string {
	result, _, _ := unstructured.NestedString(value, path...)
	return result
}
func nestedStringMap(value map[string]any, path ...string) map[string]string {
	result, _, _ := unstructured.NestedStringMap(value, path...)
	return result
}
func nestedSlice(value map[string]any, path ...string) []any {
	result, _, _ := unstructured.NestedSlice(value, path...)
	return result
}
func matches(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}
func isWorkload(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob":
		return true
	}
	return false
}
func podSpec(value map[string]any, kind string) map[string]any {
	if kind == "CronJob" {
		spec, _, _ := unstructured.NestedMap(value, "spec", "jobTemplate", "spec", "template", "spec")
		return spec
	}
	spec, _, _ := unstructured.NestedMap(value, "spec", "template", "spec")
	return spec
}

func sortGraph(graph *Graph) {
	slices.SortFunc(graph.Nodes, func(a, b Node) int {
		return strings.Compare(a.Identity.Namespace+"/"+a.Identity.Kind+"/"+a.Identity.Name, b.Identity.Namespace+"/"+b.Identity.Kind+"/"+b.Identity.Name)
	})
	slices.SortFunc(graph.Edges, func(a, b Edge) int { return strings.Compare(a.From+a.To+a.Relation, b.From+b.To+b.Relation) })
}
