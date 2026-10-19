package topology

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestResourceURLs(t *testing.T) {
	for _, tt := range []struct {
		name, kind, version, manifest string
		want                          []string
	}{
		{"ingress hosts paths and TLS", "Ingress", "networking.k8s.io/v1", `{"spec":{"tls":[{"hosts":["secure.example.com"]}],"rules":[{"host":"secure.example.com","http":{"paths":[{"path":"/app"},{"path":"/api"}]}},{"host":"plain.example.com","http":{"paths":[{"path":"/"}]}},{"host":"*.example.com","http":{"paths":[{"path":"/"}]}}]}}`, []string{"http://plain.example.com/", "https://secure.example.com/api", "https://secure.example.com/app"}},
		{"default backend IPv6 address", "Ingress", "networking.k8s.io/v1", `{"spec":{"defaultBackend":{"service":{"name":"web"}},"tls":[{}]},"status":{"loadBalancer":{"ingress":[{"ip":"2001:db8::1"}]}}}`, []string{"https://[2001:db8::1]/"}},
		{"catch-all address and path", "Ingress", "networking.k8s.io/v1", `{"spec":{"rules":[{"http":{"paths":[{"path":"/web"}]}}]},"status":{"loadBalancer":{"ingress":[{"hostname":"lb.example.com"}]}}}`, []string{"http://lb.example.com/web"}},
		{"OpenShift assigned host", "Route", "route.openshift.io/v1", `{"spec":{"path":"/app","tls":{"termination":"edge"}},"status":{"ingress":[{"host":"web.apps.example.com"},{"host":"web.apps.example.com"}]}}`, []string{"https://web.apps.example.com/app"}},
		{"OpenShift plain HTTP", "Route", "route.openshift.io/v1", `{"spec":{"host":"web.example.com"}}`, []string{"http://web.example.com/"}},
		{"wildcard TLS certificate with concrete ingress host", "Ingress", "networking.k8s.io/v1", `{"spec":{"tls":[{"hosts":["*.example.com"]}],"rules":[{"host":"web.example.com","http":{"paths":[{"path":"/"}]}}]}}`, []string{"https://web.example.com/"}},
		{"reject URI hostname injection", "Route", "route.openshift.io/v1", `{"spec":{"host":"evil.example@other.example/path"}}`, []string{}},
		{"other Route CRD", "Route", "example.io/v1", `{"spec":{"host":"web.example.com"}}`, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := ResourceURLs(core.Identity{Kind: tt.kind, APIVersion: tt.version}, parse([]byte(tt.manifest)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("URLs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObservationSpecExcludesSecrets(t *testing.T) {
	object := parse([]byte(`{"spec":{"host":"web.example.com","to":{"kind":"Service","name":"web"},"tls":{"termination":"edge","key":"PRIVATE-KEY","certificate":"CERTIFICATE","destinationCACertificate":"CA-CERT"}},"metadata":{"annotations":{"token":"SECRET-TOKEN"}}}`))
	snapshot := ObservationSpec("Route", object)
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"PRIVATE-KEY", "CERTIFICATE", "CA-CERT", "SECRET-TOKEN"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("snapshot retained %s", forbidden)
		}
	}
	if got := ResourceURLs(core.Identity{Kind: "Route", APIVersion: "route.openshift.io/v1"}, snapshot); !reflect.DeepEqual(got, []string{"https://web.example.com/"}) {
		t.Fatalf("URLs = %v", got)
	}
	workload := ObservationSpec("Deployment", parse([]byte(`{"spec":{"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"env":[{"value":"SECRET-ENV"}]}]}}}}`)))
	raw, _ = json.Marshal(workload)
	if strings.Contains(string(raw), "SECRET-ENV") || nestedString(workload, "spec", "template", "metadata", "labels", "app") != "web" {
		t.Fatalf("workload snapshot = %s", raw)
	}
}

func TestLiveRoutingOverridesDesiredAndConnectsOpenShiftAlternatives(t *testing.T) {
	route := core.Identity{APIVersion: "route.openshift.io/v1", Kind: "Route", Namespace: "demo", Name: "web"}
	ingress := core.Identity{APIVersion: "networking.k8s.io/v1", Kind: "Ingress", Namespace: "demo", Name: "entry"}
	service := core.Identity{APIVersion: "v1", Kind: "Service", Namespace: "demo", Name: "web"}
	canary := service
	canary.Name = "canary"
	plan := &store.PlanRecord{Desired: []core.Resource{{Identity: route, Manifest: []byte(`{"spec":{"host":"old.example.com","to":{"name":"old"}}}`)}}}
	graph := Build(plan, nil, []store.ObservedResource{
		{Identity: route, UID: "route", OwnerUIDs: []string{"ingress"}, TopologySpec: []byte(`{"spec":{"host":"live.example.com","to":{"name":"web","kind":"Service"},"alternateBackends":[{"kind":"Service","name":"canary"}],"tls":{"termination":"reencrypt"}}}`)},
		{Identity: ingress, UID: "ingress", TopologySpec: []byte(`{"spec":{"rules":[{"host":"live.example.com","http":{"paths":[{"backend":{"service":{"name":"web"}},"path":"/"}]}}]}}`)},
		{Identity: service}, {Identity: canary},
	})
	want := []Edge{{From: ID(ingress), To: ID(route), Relation: "owns"}, {From: ID(ingress), To: ID(service), Relation: "routes to"}, {From: ID(route), To: ID(service), Relation: "routes to"}, {From: ID(route), To: ID(canary), Relation: "routes to"}}
	for _, edge := range want {
		found := false
		for _, actual := range graph.Edges {
			found = found || actual == edge
		}
		if !found {
			t.Fatalf("missing edge %#v in %#v", edge, graph.Edges)
		}
	}
	for _, node := range graph.Nodes {
		if node.ID == ID(route) && !reflect.DeepEqual(node.URLs, []string{"https://live.example.com/"}) {
			t.Fatalf("live URLs = %v", node.URLs)
		}
	}
}

func TestWorkloadSnapshotPreservesConfigurationRelationships(t *testing.T) {
	workload := core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "web"}
	config := core.Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "demo", Name: "config"}
	graph := Build(&store.PlanRecord{Desired: []core.Resource{{Identity: workload, Manifest: []byte(`{"spec":{"template":{"spec":{"volumes":[{"configMap":{"name":"config"}}]}}}}`)}, {Identity: config}}}, nil, []store.ObservedResource{{Identity: workload, TopologySpec: []byte(`{"spec":{"template":{"metadata":{"labels":{"app":"web"}}}}}`)}})
	for _, edge := range graph.Edges {
		if edge.From == ID(workload) && edge.To == ID(config) && edge.Relation == "uses" {
			return
		}
	}
	t.Fatal("live template labels erased desired configuration relationships")
}
