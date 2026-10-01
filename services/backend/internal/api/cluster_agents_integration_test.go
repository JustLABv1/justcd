//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"github.com/justlab/justcd/services/backend/internal/clusteragent"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/security"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntegrationClusterAgent(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	key := []byte("0123456789abcdef0123456789abcdef")
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('agent-owner','agent@example.invalid'); INSERT INTO workspaces(id,name) VALUES('agent-w','Agents'),('agent-other','Other'); INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('agent-w','agent-owner','owner'); INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('agent-cluster','agent-w','Private','https://unreachable.invalid')`)
	if err != nil {
		t.Fatal(err)
	}
	enrollment, hash, _ := security.RandomToken(32)
	if err = db.ConfigureClusterAgent(ctx, "agent-cluster", "default", "", hash); err != nil {
		t.Fatal(err)
	}
	var writes atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-only-token" {
			t.Error("local credential missing")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/kube-system":
			_, _ = w.Write([]byte(`{"metadata":{"uid":"physical-cluster"}}`))
		case "/version":
			_, _ = w.Write([]byte(`{"gitVersion":"v1.35.0"}`))
		case "/api/v1/namespaces/dev/configmaps/large":
			_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]string{"name": "large", "namespace": "dev"}, "data": map[string]string{"blob": strings.Repeat("a", 3<<20)}})
		case "/api/v1/namespaces/dev/configmaps/test":
			if r.Method == "PATCH" {
				writes.Add(1)
			}
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test","namespace":"dev"},"data":{"hello":"agent"}}`))
		case "/api/v1/namespaces/dev/secrets/blocked":
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","message":"secrets forbidden","reason":"Forbidden","code":403}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer local.Close()
	server := &Server{Store: db, EncryptionKey: key}
	created := workspaceAuthorizationRequest(server.createCluster, "agent-owner", `{"workspaceId":"agent-w","name":"New agent","connectionMode":"agent"}`)
	if created.Code != 201 {
		t.Fatalf("agent creation needs a central credential: %s", created.Body.String())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agents/enroll", server.agentEnroll)
	mux.HandleFunc("POST /api/v1/agents/heartbeat", server.agentHeartbeat)
	mux.HandleFunc("GET /api/v1/agents/tasks", server.agentTasks)
	mux.HandleFunc("POST /api/v1/agents/tasks/{taskID}/result", server.agentResult)
	mux.HandleFunc("POST /api/v1/agents/renew", server.agentRenew)
	server.Mux = mux
	central := httptest.NewTLSServer(server)
	defer central.Close()
	dir := t.TempDir()
	cert, _ := x509.ParseCertificate(central.TLS.Certificates[0].Certificate[0])
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	_ = os.WriteFile(dir+"/ca.crt", ca, 0600)
	_ = os.WriteFile(dir+"/enroll", []byte(enrollment), 0600)
	a, err := clusteragent.New(clusteragent.Config{ServerURL: central.URL, ServerCAFile: dir + "/ca.crt", EnrollmentTokenFile: dir + "/enroll", IdentityFile: dir + "/identity.json", Profiles: []agentprotocol.Profile{{Name: "default", WorkspaceIDs: []string{"agent-w"}, Namespaces: []string{"dev"}}}}, &rest.Config{Host: local.URL, BearerToken: "local-only-token"})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- a.Run(runCtx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("agent failed to stop")
		}
	}()
	until := time.Now().Add(5 * time.Second)
	for {
		status, err := db.ClusterAgent(ctx, "agent-cluster")
		if err == nil && status.LastSeenAt != nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("agent did not enroll")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cluster, err := db.ClusterByID(ctx, "agent-cluster")
	if err != nil {
		t.Fatal(err)
	}
	if cluster.ConnectionMode != "agent" {
		t.Fatal("mode not exposed")
	}
	clients, err := kube.ForWorkspaceBinding(ctx, db, key, cluster, nil, false, "agent-w", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if version, err := clients.Discovery.ServerVersion(); err != nil || version.GitVersion != "v1.35.0" {
		t.Fatalf("discovery failed: %v", err)
	}
	resource := clients.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("dev")
	object, err := resource.Get(ctx, "test", metav1.GetOptions{})
	if err != nil || object.GetName() != "test" {
		t.Fatalf("private cluster read failed: %v", err)
	}
	large, err := resource.Get(ctx, "large", metav1.GetOptions{})
	if err != nil || large.GetName() != "large" {
		t.Fatalf("large encrypted response failed through HTTP middleware: %v", err)
	}
	_, err = resource.Patch(ctx, "test", "application/merge-patch+json", []byte(`{"data":{"hello":"new"}}`), metav1.PatchOptions{DryRun: []string{"All"}})
	if err != nil || writes.Load() != 1 {
		t.Fatalf("dry-run was not transported once: %v", err)
	}
	_, err = clients.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("dev").Get(ctx, "blocked", metav1.GetOptions{})
	if err == nil {
		t.Fatal("Kubernetes forbidden response lost")
	}
	_, err = clients.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("prod").Get(ctx, "test", metav1.GetOptions{})
	if err == nil {
		t.Fatal("local namespace scope bypassed")
	}
	if _, err = kube.ForWorkspaceBinding(ctx, db, key, cluster, nil, false, "agent-other", "dev"); err == nil {
		t.Fatal("cross-workspace connection accepted")
	}
	// One-use enrollment, fingerprint retention, rotation grace, and immediate revoke.
	request := func(path, token string, body any) int {
		data, _ := json.Marshal(body)
		r, _ := http.NewRequest("POST", central.URL+"/api/v1/agents"+path, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+token)
		res, err := central.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	if got := request("/enroll", enrollment, agentprotocol.Enrollment{ClusterUID: "physical-cluster", Protocol: 1, Profiles: []agentprotocol.Profile{{Name: "default"}}}); got != 401 {
		t.Fatalf("consumed enrollment accepted: %d", got)
	}
	identityData, _ := os.ReadFile(dir + "/identity.json")
	var identity agentprotocol.Identity
	_ = json.Unmarshal(identityData, &identity)
	if got := request("/renew", identity.Token, nil); got != 200 {
		t.Fatalf("renew failed: %d", got)
	}
	if _, err = db.AuthenticateClusterAgent(ctx, security.HashToken(identity.Token)); err != nil {
		t.Fatal("rotation grace missing")
	}
	if err = db.RevokeClusterAgent(ctx, "agent-cluster", false); err != nil {
		t.Fatal(err)
	}
	if got := request("/heartbeat", identity.Token, nil); got != 401 {
		t.Fatal("revoked identity accepted")
	}
	if _, err = kube.ForWorkspaceBinding(ctx, db, key, cluster, nil, false, "agent-w", "dev"); err == nil {
		t.Fatal("revoked agent fell back to direct")
	}
	_, h, _ := security.RandomToken(32)
	_ = db.ConfigureClusterAgent(ctx, "agent-cluster", "default", "", h)
	if _, err = db.EnrollClusterAgent(ctx, h, security.HashToken("new"), agentprotocol.Enrollment{ClusterUID: "different-cluster"}); err == nil {
		t.Fatal("cluster fingerprint changed")
	}
	// Claimed writes are never automatically requeued after a disconnect.
	raw, _ := security.Encrypt(key, []byte(`{}`), "agent-task:uncertain:request")
	_, _ = db.DB.ExecContext(ctx, `UPDATE cluster_agents SET token_hash=$2,token_expires_at=NOW()+INTERVAL '1 day',last_seen_at=NOW() WHERE cluster_id=$1`, "agent-cluster", security.HashToken("test"))
	if err = db.QueueAgentTask(ctx, "uncertain", "agent-cluster", raw, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.ClaimAgentTask(ctx, "agent-cluster", "lease-one"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.ClaimAgentTask(ctx, "agent-cluster", "lease-two"); err == nil {
		t.Fatal("claimed write replayed")
	}
	if err = db.CompleteAgentTask(ctx, "agent-cluster", "uncertain", "wrong-lease", raw); err == nil {
		t.Fatal("wrong lease accepted")
	}
	if err = db.CompleteAgentTask(ctx, "other-cluster", "uncertain", "lease-one", raw); err == nil {
		t.Fatal("cross-cluster result accepted")
	}
	db.DeleteAgentTask(ctx, "uncertain")

	// An uncertain write stops automatic reconciliation until a user reviews state.
	_, err = db.DB.ExecContext(ctx, `INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('agent-source','agent-w','Git','https://example.invalid/app.git'); INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy) VALUES('agent-app','agent-w','App','agent-source','main','deploy','yaml','agent-cluster','[{"namespace":"dev"}]','auto-safe'); INSERT INTO operations(id,application_id,actor_id,status) VALUES('agent-unknown','agent-app','agent-owner','running')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ConfigureClusterAgent(ctx, "agent-cluster", "default", "", h); err == nil {
		t.Fatal("connection changed during a running deployment")
	}
	if err = db.FinishOperationWithRetry(ctx, "agent-unknown", "agent-app", "unknown outcome", 1, "cluster.agent_unknown_outcome", nil, "unknown_requires_review"); err != nil {
		t.Fatal(err)
	}
	var paused bool
	if err = db.DB.QueryRowContext(ctx, `SELECT auto_sync_paused FROM applications WHERE id='agent-app'`).Scan(&paused); err != nil || !paused {
		t.Fatalf("uncertain write did not pause automatic reconciliation: %v", err)
	}
}
