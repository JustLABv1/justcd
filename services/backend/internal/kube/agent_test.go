package kube

import (
	"context"
	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"github.com/justlab/justcd/services/backend/internal/clusteragent"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUnknownWritesRequireReview(t *testing.T) {
	for _, tc := range []struct {
		method, uri string
		transient   bool
	}{{"PATCH", "/api/v1/namespaces/dev/configmaps/x", false}, {"PATCH", "/api/v1/namespaces/dev/configmaps/x?dryRun=All", true}, {"GET", "/api/v1/namespaces/dev/configmaps/x", true}, {"POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", true}} {
		r := httptest.NewRequest(tc.method, tc.uri, nil)
		err := uncertainAgentError(r, "request outcome unknown").(*AgentError)
		if err.Retryable() != tc.transient {
			t.Fatalf("%s %s incorrectly retryable", tc.method, tc.uri)
		}
	}
}

func TestPromotedBootstrapIsRevalidatedByLocalAgent(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"kind":"Project","metadata":{"name":"justapps-prd"}}`))
	}))
	defer upstream.Close()
	enrolled := agentprotocol.Profile{Name: "default", WorkspaceIDs: []string{"w"}, Namespaces: []string{"*"}, ClusterScope: true}
	a := &clusteragent.Agent{Config: clusteragent.Config{Profiles: []agentprotocol.Profile{enrolled}}, Local: map[string]*http.Client{"default": upstream.Client()}, KubernetesURL: upstream.URL}
	task := agentprotocol.Request{Profile: "default", WorkspaceID: "w", Namespace: "justapps-prd", Method: "POST", URI: "/apis/project.openshift.io/v1/projectrequests", Body: []byte(`{"apiVersion":"project.openshift.io/v1","kind":"ProjectRequest","metadata":{"name":"justapps-prd"}}`), Deadline: time.Now().Add(time.Minute)}
	if err := routeProjectBootstrap(&task, &enrolled); err != nil || !task.ClusterScope {
		t.Fatalf("backend route failed: %+v %v", task, err)
	}
	if result := a.Execute(context.Background(), task); result.Error != "" || result.Status != http.StatusCreated {
		t.Fatalf("agent rejected valid backend route: %+v", result)
	}
	// Change local policy after enrollment. The backend still has the wildcard,
	// but a new task must not reach Kubernetes under the narrowed local policy.
	a.Config.Profiles[0].Namespaces = []string{"different-namespace"}
	if result := a.Execute(context.Background(), task); result.ErrorCode != "cluster.agent_scope_denied" || calls != 1 {
		t.Fatalf("stale enrollment bypassed current agent policy: %+v calls=%d", result, calls)
	}
}

func TestProjectBootstrapUsesClusterRouteWithSameWorkloadProfile(t *testing.T) {
	profile := agentprotocol.Profile{Name: "default", WorkspaceIDs: []string{"w"}, Namespaces: []string{"*"}, ClusterScope: true, DeniedNamespaces: []string{"agent"}}
	for _, tc := range []struct {
		name, method, uri, namespace, project string
		promoted, denied                      bool
	}{
		{"existing project", "GET", "/apis/project.openshift.io/v1/projects/justapps-prd", "justapps-prd", "", true, false},
		{"missing project", "POST", "/apis/project.openshift.io/v1/projectrequests", "justapps-prd", "justapps-prd", true, false},
		{"different project", "POST", "/apis/project.openshift.io/v1/projectrequests", "justapps-prd", "other", false, true},
		{"protected project", "POST", "/apis/project.openshift.io/v1/projectrequests", "agent", "agent", false, true},
		{"invalid request", "POST", "/apis/project.openshift.io/v1/projectrequests", "justapps-prd", "", false, true},
		{"ordinary workload", "GET", "/api/v1/namespaces/justapps-prd/pods", "justapps-prd", "", false, false},
		{"unrelated cluster resource", "GET", "/api/v1/nodes", "justapps-prd", "", false, false},
		{"project collection", "GET", "/apis/project.openshift.io/v1/projects", "justapps-prd", "", false, false},
		{"different project lookup", "GET", "/apis/project.openshift.io/v1/projects/other", "justapps-prd", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := agentprotocol.Request{Profile: "default", WorkspaceID: "w", Namespace: tc.namespace, Method: tc.method, URI: tc.uri, Deadline: time.Now().Add(time.Minute)}
			if tc.method == "POST" {
				task.Body = []byte(`{"apiVersion":"project.openshift.io/v1","kind":"ProjectRequest","metadata":{"name":"` + tc.project + `"}}`)
			}
			err := routeProjectBootstrap(&task, &profile)
			if (err != nil) != tc.denied || task.ClusterScope != tc.promoted || task.Profile != "default" || task.Namespace != tc.namespace {
				t.Fatalf("incorrect bootstrap route: %+v err=%v", task, err)
			}
		})
	}
}

func TestProjectBootstrapDoesNotPromoteRestrictedProfiles(t *testing.T) {
	for _, p := range []*agentprotocol.Profile{nil, {Name: "default"}, {Name: "default", ClusterScope: true, DeploymentCredentials: &agentprotocol.DeploymentCredentials{LifetimeSeconds: 600}}} {
		task := agentprotocol.Request{Profile: "default", Namespace: "dev", Method: "GET", URI: "/apis/project.openshift.io/v1/projects/dev"}
		if err := routeProjectBootstrap(&task, p); err != nil || task.ClusterScope {
			t.Fatalf("restricted profile promoted: %+v err=%v", p, err)
		}
	}
}
