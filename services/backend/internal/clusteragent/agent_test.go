package clusteragent

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"k8s.io/client-go/rest"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecuteKeepsCredentialsLocalAndDoesNotFollowRedirect(t *testing.T) {
	calls := 0
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Error("task supplied upstream credential")
		}
		if r.URL.Path == "/api/v1/namespaces/dev/configmaps/redirect" {
			w.Header().Set("Location", "https://evil.invalid")
			w.WriteHeader(302)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"reason":"Forbidden"}`))
	}))
	defer local.Close()
	a := &Agent{Config: Config{Profiles: []agentprotocol.Profile{{Name: "default", WorkspaceIDs: []string{"w"}, Namespaces: []string{"dev"}}}}, Local: map[string]*http.Client{"default": {CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, KubernetesURL: local.URL}
	task := agentprotocol.Request{Profile: "default", WorkspaceID: "w", Namespace: "dev", Method: "GET", URI: "/api/v1/namespaces/dev/configmaps/test", Deadline: time.Now().Add(time.Minute)}
	result := a.Execute(context.Background(), task)
	if result.Status != 403 || result.Error != "" {
		t.Fatalf("permission status lost: %+v", result)
	}
	task.URI = "/api/v1/namespaces/prod/secrets"
	if a.Execute(context.Background(), task).Error == "" || calls != 1 {
		t.Fatal("scope escape reached Kubernetes")
	}
	task.URI = "/api/v1/namespaces/dev/configmaps/redirect"
	if got := a.Execute(context.Background(), task); got.Status != 302 {
		t.Fatalf("redirect followed: %+v", got)
	}
}
func TestIdentityPersistsWithPrivatePermissions(t *testing.T) {
	a := &Agent{Config: Config{IdentityFile: t.TempDir() + "/identity.json"}}
	if err := a.saveIdentity(agentprotocol.Identity{Token: "identity-secret", ClusterID: "cluster"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(a.Config.IdentityFile)
	if err != nil {
		t.Fatal(err)
	}
	var identity agentprotocol.Identity
	if err = json.Unmarshal(data, &identity); err != nil || identity.Token != "identity-secret" {
		t.Fatalf("identity not restored: %v", err)
	}
	info, err := os.Stat(a.Config.IdentityFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("identity permissions: %v", err)
	}
}

func TestStoredIdentityCannotMoveClusters(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"uid":"different-cluster"}}`))
	}))
	defer local.Close()
	a := &Agent{Config: Config{IdentityFile: t.TempDir() + "/identity.json", Profiles: []agentprotocol.Profile{{Name: "default"}}}, Local: map[string]*http.Client{"default": local.Client()}, KubernetesURL: local.URL}
	if err := a.saveIdentity(agentprotocol.Identity{Token: "identity-secret", ClusterUID: "original-cluster"}); err != nil {
		t.Fatal(err)
	}
	if err := a.initialize(context.Background()); err == nil {
		t.Fatal("stored identity accepted on another physical cluster")
	}
}

func TestClusterIdentityUsesCustomCAAndProfileToken(t *testing.T) {
	local := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/kube-system" || r.Header.Get("Authorization") != "Bearer custom-token" {
			t.Errorf("unexpected identity request: %s, authorization present: %t", r.URL.Path, r.Header.Get("Authorization") != "")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"metadata":{"uid":"physical-cluster"}}`))
	}))
	defer local.Close()
	dir := t.TempDir()
	caFile, tokenFile := dir+"/ca.crt", dir+"/token"
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: local.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("custom-token"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ServerURL: "https://justcd.invalid", EnrollmentTokenFile: dir + "/enrollment", IdentityFile: dir + "/identity", KubernetesCAFile: caFile, KubernetesServerURL: local.URL, Profiles: []agentprotocol.Profile{{Name: "default", WorkspaceIDs: []string{"w"}, TokenFile: tokenFile}}}
	// The explicitly configured CA must replace inherited in-cluster CA data.
	kube := &rest.Config{Host: "https://unused.invalid", BearerToken: "pod-token", TLSClientConfig: rest.TLSClientConfig{CAData: []byte("invalid inherited CA")}}
	a, err := New(cfg, kube)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := a.clusterUID(context.Background())
	if err != nil || uid != "physical-cluster" {
		t.Fatalf("identity: %q, %v", uid, err)
	}
	cfg.KubernetesCAFile = ""
	kube.TLSClientConfig.CAData = nil
	a, err = New(cfg, kube)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.clusterUID(context.Background()); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("TLS failure cause missing: %v", err)
	}
}

func TestRejectUnsafeKubernetesEndpoint(t *testing.T) {
	for _, endpoint := range []string{"http://kube.invalid", "https://user:password@kube.invalid", "https://kube.invalid?token=secret", "https://kube.invalid#fragment"} {
		_, err := New(Config{ServerURL: "https://justcd.invalid", KubernetesServerURL: endpoint, IdentityFile: "identity", EnrollmentTokenFile: "enrollment", Profiles: []agentprotocol.Profile{{Name: "default", WorkspaceIDs: []string{"w"}}}}, &rest.Config{})
		if err == nil {
			t.Errorf("unsafe endpoint accepted: %s", endpoint)
		}
	}
}

func TestConfiguredClusterIdentityEnrollsAndRestartsWithoutKubernetesAccess(t *testing.T) {
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("configured cluster identity must not contact Kubernetes")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer kube.Close()
	enrollments := 0
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enrollments++
		if r.URL.Path != "/api/v1/agents/enroll" || r.Header.Get("Authorization") != "Bearer enrollment-token" {
			t.Errorf("unexpected enrollment request: %s", r.URL.Path)
		}
		var enrollment agentprotocol.Enrollment
		if err := json.NewDecoder(r.Body).Decode(&enrollment); err != nil {
			t.Fatal(err)
		}
		if enrollment.ClusterUID != "operator-cluster" {
			t.Errorf("identity: %q", enrollment.ClusterUID)
		}
		_ = json.NewEncoder(w).Encode(agentprotocol.Identity{Token: "identity-token", ClusterUID: enrollment.ClusterUID})
	}))
	defer central.Close()
	dir := t.TempDir()
	cfg := Config{ClusterID: "operator-cluster", ServerURL: central.URL, IdentityFile: dir + "/identity.json", EnrollmentTokenFile: dir + "/enrollment", Profiles: []agentprotocol.Profile{{Name: "default"}}}
	if err := os.WriteFile(cfg.EnrollmentTokenFile, []byte("enrollment-token"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &Agent{Config: cfg, HTTP: central.Client(), KubernetesURL: kube.URL, Local: map[string]*http.Client{"default": kube.Client()}}
	if err := a.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := &Agent{Config: cfg}
	if err := restarted.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if enrollments != 1 {
		t.Fatalf("enrollment count: %d", enrollments)
	}
	restarted.Config.ClusterID = "different-cluster"
	if err := restarted.initialize(context.Background()); err == nil {
		t.Fatal("changed configured identity accepted")
	}
}

func TestRejectInvalidConfiguredClusterIdentity(t *testing.T) {
	for _, id := range []string{" cluster", "cluster ", "\n", strings.Repeat("a", 129)} {
		_, err := New(Config{ServerURL: "https://justcd.invalid", ClusterID: id, IdentityFile: "identity", EnrollmentTokenFile: "enrollment", Profiles: []agentprotocol.Profile{{Name: "default", WorkspaceIDs: []string{"w"}}}}, &rest.Config{})
		if err == nil {
			t.Error("invalid cluster identity accepted")
		}
	}
}

func TestExecuteBoundOpenShiftBootstrap(t *testing.T) {
	calls := 0
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"kind":"Status","reason":"Forbidden"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"kind":"Project","metadata":{"name":"preview-app"}}`))
	}))
	defer local.Close()
	a := &Agent{Config: Config{Profiles: []agentprotocol.Profile{{Name: "workload", WorkspaceIDs: []string{"w"}, Namespaces: []string{"preview-*"}, ClusterScope: true, DeniedNamespaces: []string{"preview-agent"}}}}, Local: map[string]*http.Client{"workload": local.Client()}, KubernetesURL: local.URL}
	task := agentprotocol.Request{Profile: "workload", WorkspaceID: "w", Namespace: "preview-app", Method: "GET", URI: "/apis/project.openshift.io/v1/projects/preview-app", Deadline: time.Now().Add(time.Minute)}
	if result := a.Execute(context.Background(), task); result.Error != "" || result.Status != http.StatusForbidden {
		t.Fatalf("project lookup was blocked by agent: %+v", result)
	}
	task.Method = "POST"
	task.URI = "/apis/project.openshift.io/v1/projectrequests"
	task.ContentType = "application/json"
	task.Body = []byte(`{"apiVersion":"project.openshift.io/v1","kind":"ProjectRequest","metadata":{"name":"preview-app"}}`)
	if result := a.Execute(context.Background(), task); result.Error != "" || result.Status != http.StatusCreated {
		t.Fatalf("project request was blocked by agent: %+v", result)
	}
	for _, target := range []string{"preview-other", "preview-agent"} {
		task.Body = []byte(`{"apiVersion":"project.openshift.io/v1","kind":"ProjectRequest","metadata":{"name":"` + target + `"}}`)
		if result := a.Execute(context.Background(), task); result.ErrorCode != "cluster.agent_scope_denied" {
			t.Fatalf("scope escape accepted: %+v", result)
		}
	}
	if calls != 2 {
		t.Fatalf("out-of-scope request reached API: calls=%d", calls)
	}
}

func TestExecuteWildcardProfileOpenShiftBootstrapAndScopeDiagnostics(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	calls := 0
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer local.Close()
	profile := agentprotocol.Profile{Name: "default", WorkspaceIDs: []string{"2009cb82c12aafa7da94ce15ed637093"}, Namespaces: []string{"*"}, ClusterScope: true}
	a := &Agent{Config: Config{Profiles: []agentprotocol.Profile{profile}}, Local: map[string]*http.Client{"default": local.Client()}, KubernetesURL: local.URL}
	task := agentprotocol.Request{Profile: "default", WorkspaceID: profile.WorkspaceIDs[0], Namespace: "justapps-prd", Method: "GET", URI: "/apis/project.openshift.io/v1/projects/justapps-prd", Deadline: time.Now().Add(time.Minute)}
	if result := a.Execute(context.Background(), task); result.Error != "" {
		t.Fatalf("wildcard profile project lookup failed: %+v", result)
	}
	task.Method = "POST"
	task.URI = "/apis/project.openshift.io/v1/projectrequests"
	task.Body = []byte(`{"apiVersion":"project.openshift.io/v1","kind":"ProjectRequest","metadata":{"name":"justapps-prd"}}`)
	if result := a.Execute(context.Background(), task); result.Error != "" {
		t.Fatalf("wildcard profile project creation failed: %+v", result)
	}
	// A different cluster API is still rejected rather than silently promoting
	// all namespace-routed requests to cluster scope.
	task.Method = "GET"
	task.URI = "/api/v1/nodes?labelSelector=private-value"
	task.Body = nil
	result := a.Execute(context.Background(), task)
	if result.ErrorCode != "cluster.agent_scope_denied" || !strings.Contains(result.Error, "namespace-scoped route") || calls != 2 {
		t.Fatalf("unrestricted cluster request accepted: %+v calls=%d", result, calls)
	}
	var warning map[string]any
	if err := json.Unmarshal(logs.Bytes(), &warning); err != nil {
		t.Fatal(err)
	}
	if warning["method"] != "GET" || warning["path"] != "/api/v1/nodes" || warning["clusterScope"] != false || warning["profileClusterScope"] != true || strings.Contains(logs.String(), "private-value") {
		t.Fatalf("scope diagnostic missing context or leaked query: %s", logs.String())
	}
}
