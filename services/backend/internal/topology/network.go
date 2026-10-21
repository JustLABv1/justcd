package topology

import (
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ObservationSpec retains only topology inputs. In particular, Route TLS keys,
// certificates, annotations, and literal workload container/environment data are excluded.
func ObservationSpec(kind string, object map[string]any) map[string]any {
	result := map[string]any{}
	if isWorkload(kind) || kind == "ReplicaSet" || kind == "Pod" || kind == "RoleBinding" || kind == "ClusterRoleBinding" {
		result["references"] = observationReferences(kind, object)
	}
	if slices.Contains([]string{"Ingress", "Route", "Service", "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job", "CronJob"}, kind) {
		result["spec"] = map[string]any{}
	}
	copyField := func(path ...string) {
		if value, found, err := unstructured.NestedFieldCopy(object, path...); err == nil && found {
			_ = unstructured.SetNestedField(result, value, path...)
		}
	}
	switch kind {
	case "Ingress":
		copyField("spec", "rules")
		copyField("spec", "defaultBackend")
		for _, tls := range nestedSlice(object, "spec", "tls") {
			if entry, ok := tls.(map[string]any); ok {
				entries := nestedSlice(result, "spec", "tls")
				hosts := nestedSlice(entry, "hosts")
				if hosts == nil {
					hosts = []any{}
				}
				_ = unstructured.SetNestedSlice(result, append(entries, map[string]any{"hosts": hosts}), "spec", "tls")
			}
		}
		for _, address := range nestedSlice(object, "status", "loadBalancer", "ingress") {
			if entry, ok := address.(map[string]any); ok {
				entries := nestedSlice(result, "status", "loadBalancer", "ingress")
				_ = unstructured.SetNestedSlice(result, append(entries, map[string]any{"hostname": nestedString(entry, "hostname"), "ip": nestedString(entry, "ip")}), "status", "loadBalancer", "ingress")
			}
		}
	case "Route":
		for _, path := range [][]string{{"spec", "host"}, {"spec", "path"}, {"spec", "to"}, {"spec", "port"}, {"spec", "alternateBackends"}, {"spec", "tls", "termination"}, {"spec", "tls", "insecureEdgeTerminationPolicy"}} {
			copyField(path...)
		}
		for _, ingress := range nestedSlice(object, "status", "ingress") {
			if entry, ok := ingress.(map[string]any); ok {
				entries := nestedSlice(result, "status", "ingress")
				conditions := []any{}
				for _, raw := range nestedSlice(entry, "conditions") {
					if condition, ok := raw.(map[string]any); ok {
						conditions = append(conditions, map[string]any{"type": nestedString(condition, "type"), "status": nestedString(condition, "status"), "reason": nestedString(condition, "reason")})
					}
				}
				_ = unstructured.SetNestedSlice(result, append(entries, map[string]any{"host": nestedString(entry, "host"), "conditions": conditions}), "status", "ingress")
			}
		}
	case "Service":
		copyField("spec", "selector")
		copyField("spec", "ports")
		copyField("spec", "type")
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job":
		copyField("spec", "template", "metadata", "labels")
	case "CronJob":
		copyField("spec", "jobTemplate", "spec", "template", "metadata", "labels")
	}
	return result
}

var dnsHost = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

// ResourceURLs exposes configured host/path pairs, never a wildcard hostname or
// an arbitrary URI supplied by a manifest. A URL is not a reachability assertion.
func ResourceURLs(identity core.Identity, object map[string]any) []string {
	urls := []string{}
	add := func(scheme, host, path string) {
		if host == "" || (!dnsHost.MatchString(host) && net.ParseIP(host) == nil) {
			return
		}
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		if path == "" {
			path = "/"
		}
		if !strings.HasPrefix(path, "/") {
			return
		}
		value := (&url.URL{Scheme: scheme, Host: host, Path: path}).String()
		if !slices.Contains(urls, value) {
			urls = append(urls, value)
		}
	}
	switch {
	case identity.Kind == "Ingress" && strings.HasPrefix(identity.APIVersion, "networking.k8s.io/"):
		addresses := []string{}
		for _, item := range nestedSlice(object, "status", "loadBalancer", "ingress") {
			if address, ok := item.(map[string]any); ok {
				host := nestedString(address, "hostname")
				if host == "" {
					host = nestedString(address, "ip")
				}
				addresses = append(addresses, host)
			}
		}
		schemeFor := func(host string) string {
			for _, item := range nestedSlice(object, "spec", "tls") {
				if tls, ok := item.(map[string]any); ok {
					hosts := nestedSlice(tls, "hosts")
					if len(hosts) == 0 {
						return "https"
					}
					for _, candidate := range hosts {
						if candidate == host {
							return "https"
						}
						if pattern, ok := candidate.(string); ok && strings.HasPrefix(pattern, "*.") {
							suffix := strings.TrimPrefix(pattern, "*")
							prefix := strings.TrimSuffix(host, suffix)
							if prefix != host && prefix != "" && !strings.Contains(prefix, ".") {
								return "https"
							}
						}
					}
				}
			}
			return "http"
		}
		for _, item := range nestedSlice(object, "spec", "rules") {
			rule, ok := item.(map[string]any)
			if !ok {
				continue
			}
			hosts := []string{nestedString(rule, "host")}
			if hosts[0] == "" {
				hosts = addresses
			}
			for _, item := range nestedSlice(rule, "http", "paths") {
				if path, ok := item.(map[string]any); ok {
					for _, host := range hosts {
						add(schemeFor(host), host, nestedString(path, "path"))
					}
				}
			}
		}
		if backend, _, _ := unstructured.NestedMap(object, "spec", "defaultBackend"); len(backend) > 0 {
			for _, host := range addresses {
				add(schemeFor(host), host, "/")
			}
		}
	case identity.Kind == "Route" && strings.HasPrefix(identity.APIVersion, "route.openshift.io/"):
		scheme := "http"
		if nestedString(object, "spec", "tls", "termination") != "" {
			scheme = "https"
		}
		hosts := []string{nestedString(object, "spec", "host")}
		for _, item := range nestedSlice(object, "status", "ingress") {
			if ingress, ok := item.(map[string]any); ok {
				hosts = append(hosts, nestedString(ingress, "host"))
			}
		}
		for _, host := range hosts {
			add(scheme, host, nestedString(object, "spec", "path"))
		}
	}
	slices.Sort(urls)
	return urls
}
