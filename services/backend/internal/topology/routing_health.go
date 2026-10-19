package topology

import (
	"fmt"
	"slices"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
)

type BuildOptions struct {
	ObservationIncomplete bool
}

type RoutingHealth struct {
	Status    string          `json:"status"`
	Reason    string          `json:"reason"`
	Label     string          `json:"label"`
	Message   string          `json:"message"`
	ReadyPods int             `json:"readyPods"`
	TotalPods int             `json:"totalPods"`
	Available *bool           `json:"available,omitempty"`
	Backends  []BackendHealth `json:"backends,omitempty"`
}

type BackendHealth struct {
	ServiceName string `json:"serviceName"`
	Namespace   string `json:"namespace"`
	Port        string `json:"port,omitempty"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
	ReadyPods   int    `json:"readyPods"`
	TotalPods   int    `json:"totalPods"`
}

type backendReference struct{ name, port string }

func networkBackends(id core.Identity, object map[string]any) []backendReference {
	refs := []backendReference{}
	add := func(name, port string) {
		ref := backendReference{name, port}
		if name != "" && !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	switch {
	case id.Kind == "Ingress" && strings.HasPrefix(id.APIVersion, "networking.k8s.io/"):
		addIngress := func(backend map[string]any) {
			name := nestedString(backend, "service", "name")
			port := nestedString(backend, "service", "port", "name")
			if port == "" {
				if value, ok := backend["service"].(map[string]any); ok {
					if p, ok := value["port"].(map[string]any); ok && p["number"] != nil {
						port = fmt.Sprint(p["number"])
					}
				}
			}
			add(name, port)
		}
		if backend, ok := object["spec"].(map[string]any); ok {
			if value, ok := backend["defaultBackend"].(map[string]any); ok {
				addIngress(value)
			}
		}
		for _, entry := range nestedSlice(object, "spec", "rules") {
			if rule, ok := entry.(map[string]any); ok {
				for _, entry := range nestedSlice(rule, "http", "paths") {
					if path, ok := entry.(map[string]any); ok {
						if backend, ok := path["backend"].(map[string]any); ok {
							addIngress(backend)
						}
					}
				}
			}
		}
	case id.Kind == "Route" && strings.HasPrefix(id.APIVersion, "route.openshift.io/"):
		port := ""
		if spec, ok := object["spec"].(map[string]any); ok {
			if target, ok := spec["port"].(map[string]any); ok && target["targetPort"] != nil {
				port = fmt.Sprint(target["targetPort"])
			}
		}
		backends := append([]any{}, nestedSlice(object, "spec", "alternateBackends")...)
		if spec, ok := object["spec"].(map[string]any); ok {
			backends = append(backends, spec["to"])
		}
		for _, entry := range backends {
			if backend, ok := entry.(map[string]any); ok {
				kind := nestedString(backend, "kind")
				if (kind == "" || kind == "Service") && fmt.Sprint(backend["weight"]) != "0" {
					add(nestedString(backend, "name"), port)
				}
			}
		}
	}
	return refs
}

func assessRouting(items map[string]*material, byName map[string]*material, options BuildOptions) {
	hasEvidence := false
	for _, item := range items {
		if item.node.Source == "kubernetes" {
			hasEvidence = true
		}
	}
	complete := hasEvidence && !options.ObservationIncomplete
	makeHealth := func(status, reason, label, message string, ready, total int, available *bool) *RoutingHealth {
		return &RoutingHealth{Status: status, Reason: reason, Label: label, Message: message, ReadyPods: ready, TotalPods: total, Available: available}
	}
	boolean := func(value bool) *bool { return &value }
	for _, item := range items {
		if item.node.Identity.Kind != "Service" {
			continue
		}
		health := makeHealth("unknown", "ObservationIncomplete", "Readiness unknown", "A complete pod observation is not available.", 0, 0, nil)
		if !complete {
			item.node.RoutingHealth = health
			continue
		}
		live := item.node.Source == "kubernetes"
		if !live {
			item.node.RoutingHealth = makeHealth("degraded", "ServiceMissing", "Service missing", "The referenced Service was not found in the latest observation.", 0, 0, boolean(false))
			continue
		}
		selector := nestedStringMap(item.value, "spec", "selector")
		if len(selector) == 0 {
			item.node.RoutingHealth = makeHealth("unknown", "SelectorUnavailable", "Readiness unknown", "This Service has no pod selector; pod-based backend readiness cannot be verified.", 0, 0, nil)
			continue
		}
		ready, total, starting := 0, 0, 0
		for _, pod := range items {
			if pod.node.Identity.Kind != "Pod" || pod.node.Identity.Namespace != item.node.Identity.Namespace || !matches(selector, pod.labels) {
				continue
			}
			if pod.node.Source != "kubernetes" {
				continue
			}
			total++
			terminating := pod.node.HealthSummary != nil && pod.node.HealthSummary.Terminating
			failed := pod.node.Phase == "Failed" || pod.node.HealthSummary != nil && pod.node.HealthSummary.FailureReason != ""
			if pod.node.Phase == "Running" && pod.node.Readiness == "Ready" && !terminating && !failed {
				ready++
			}
			if !failed && !terminating && (pod.node.Phase == "Pending" || pod.node.Phase == "Running" && pod.node.Readiness != "Ready") {
				starting++
			}
		}
		switch {
		case ready > 0 && ready == total:
			health = makeHealth("healthy", "ReadyBackends", "Backend ready", fmt.Sprintf("%d matching pods are ready to receive traffic.", ready), ready, total, boolean(true))
		case ready > 0:
			health = makeHealth("progressing", "PartialReadiness", "Partially ready", fmt.Sprintf("%d of %d matching pods are ready; traffic has a ready backend while other pods recover.", ready, total), ready, total, boolean(true))
		case starting > 0:
			health = makeHealth("progressing", "PodsStarting", "Waiting for pods", fmt.Sprintf("None of %d matching pods are ready yet; pods are starting.", total), ready, total, boolean(false))
		case total == 0:
			health = makeHealth("degraded", "NoMatchingPods", "No matching pods", "The Service selector does not match any observed pods.", ready, total, boolean(false))
		default:
			health = makeHealth("degraded", "NoReadyPods", "No ready pods", fmt.Sprintf("None of %d matching pods can receive traffic.", total), ready, total, boolean(false))
		}
		item.node.RoutingHealth = health
	}
	for _, item := range items {
		refs := networkBackends(item.node.Identity, item.value)
		if len(refs) == 0 {
			continue
		}
		health := makeHealth("healthy", "ReadyBackends", "Backend ready", "All configured Service backends have ready pods. External reachability has not been tested.", 0, 0, boolean(true))
		countedServices := map[string]bool{}
		for _, ref := range refs {
			service := byName[nameKey("Service", item.node.Identity.Namespace, ref.name)]
			if service == nil || service.node.RoutingHealth == nil {
				continue
			}
			backend := *service.node.RoutingHealth
			if ref.port != "" && (backend.Status == "healthy" || backend.Status == "progressing") && complete {
				found := false
				for _, entry := range nestedSlice(service.value, "spec", "ports") {
					if port, ok := entry.(map[string]any); ok {
						if fmt.Sprint(port["name"]) == ref.port || fmt.Sprint(port["port"]) == ref.port || item.node.Identity.Kind == "Route" && fmt.Sprint(port["targetPort"]) == ref.port {
							found = true
						}
					}
				}
				spec, _ := service.value["spec"].(map[string]any)
				if _, known := spec["ports"]; !known {
					backend = *makeHealth("unknown", "ServicePortsUnavailable", "Port readiness unknown", "The Service port configuration has not been observed yet.", backend.ReadyPods, backend.TotalPods, nil)
				} else if !found {
					backend = *makeHealth("degraded", "ServicePortMissing", "Service port missing", fmt.Sprintf("Service %s has no configured port %s.", ref.name, ref.port), 0, backend.TotalPods, boolean(false))
				}
			}
			health.Backends = append(health.Backends, BackendHealth{ServiceName: ref.name, Namespace: item.node.Identity.Namespace, Port: ref.port, Status: backend.Status, Reason: backend.Reason, Message: backend.Message, ReadyPods: backend.ReadyPods, TotalPods: backend.TotalPods})
			if !countedServices[service.node.ID] {
				health.ReadyPods += service.node.RoutingHealth.ReadyPods
				health.TotalPods += service.node.RoutingHealth.TotalPods
				countedServices[service.node.ID] = true
			}
			rank := map[string]int{"healthy": 0, "progressing": 1, "unknown": 2, "degraded": 3}
			if rank[backend.Status] > rank[health.Status] {
				health.Status = backend.Status
				health.Reason = backend.Reason
				health.Label = backend.Label
				health.Message = fmt.Sprintf("Service %s: %s", ref.name, backend.Message)
			}
			if backend.Available == nil {
				health.Available = nil
			} else if !*backend.Available {
				health.Available = boolean(false)
			}
		}
		if !complete {
			health.Status = "unknown"
			health.Reason = "ObservationIncomplete"
			health.Label = "Readiness unknown"
			health.Message = "The routing observation is incomplete; missing backends cannot be confirmed."
			health.Available = nil
		}
		if item.node.Identity.Kind == "Route" && complete {
			admitted, rejected := false, false
			for _, entry := range nestedSlice(item.value, "status", "ingress") {
				if ingress, ok := entry.(map[string]any); ok {
					for _, entry := range nestedSlice(ingress, "conditions") {
						if condition, ok := entry.(map[string]any); ok && nestedString(condition, "type") == "Admitted" {
							admitted = admitted || nestedString(condition, "status") == "True"
							rejected = rejected || nestedString(condition, "status") == "False"
						}
					}
				}
			}
			if !admitted && rejected {
				health.Status = "degraded"
				health.Reason = "RouteNotAdmitted"
				health.Label = "Route rejected"
				health.Message = "The OpenShift router has rejected this Route."
				health.Available = boolean(false)
			} else if !admitted && health.Status == "healthy" {
				health.Status = "progressing"
				health.Reason = "AwaitingAdmission"
				health.Label = "Awaiting router"
				health.Message = "Backend pods are ready; the OpenShift router has not reported admission yet."
				health.Available = nil
			}
		}
		item.node.RoutingHealth = health
	}
}
