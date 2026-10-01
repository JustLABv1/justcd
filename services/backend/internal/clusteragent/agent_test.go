package clusteragent

import (
	"context"
	"encoding/json"
	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"net/http"
	"net/http/httptest"
	"os"
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
