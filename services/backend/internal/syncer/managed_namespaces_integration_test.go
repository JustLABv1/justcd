//go:build integration

package syncer

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"github.com/justlab/justcd/services/backend/internal/clusteragent"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/client-go/rest"
)

func managedNamespaceApplication(id string) store.Application {
	return store.Application{ID: id, WorkspaceID: "workspace", SourceID: "source", Name: id, Revision: "main", ManifestPath: "yaml", Renderer: "yaml", ClusterID: "cluster", Namespaces: []store.NamespaceBinding{{Namespace: "preview-1"}}, CreateNamespaces: true, SyncPolicy: "manual", PollSeconds: 300}
}

func TestIntegrationManagedNamespaceProvisionedBeforePlanning(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	app := managedNamespaceApplication("managed-app")
	if err := db.CreateNamespaceBinding(ctx, app.WorkspaceID, app.ClusterID, "preview-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	profile := agentprotocol.Profile{Name: "managed", WorkspaceIDs: []string{"workspace"}, Namespaces: []string{"preview-*"}, DeploymentCredentials: &agentprotocol.DeploymentCredentials{LifetimeSeconds: 600, Provisioning: &agentprotocol.NamespaceProvisioning{DeploymentClusterRole: "approved", DeleteNamespaces: true}}}
	profiles, _ := json.Marshal([]agentprotocol.Profile{profile})
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO cluster_agents(cluster_id,default_profile,profiles,token_expires_at,last_seen_at) VALUES('cluster','managed',$1,NOW()+INTERVAL '1 day',NOW())`, profiles); err != nil {
		t.Fatal(err)
	}
	legacyWorkspace := "workspace"
	if err := db.CreateCredential(ctx, store.Credential{ID: "legacy-broad", WorkspaceID: &legacyWorkspace, Name: "Unused legacy cluster credential", Kind: "kubernetes-token", Cipher: []byte("must-not-be-used")}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE clusters SET cluster_scope_credential_id='legacy-broad' WHERE id='cluster'; UPDATE cluster_agents SET cluster_profile='old-privileged-profile' WHERE cluster_id='cluster'`); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	objects := map[string]map[string]any{}
	namespaceCreates, workloads, deletes := 0, 0, 0
	local := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		workload := r.URL.Path == "/version" || strings.Contains(r.URL.Path, "/configmaps") || r.URL.Path == "/api" || r.URL.Path == "/apis" || r.URL.Path == "/api/v1"
		expected := "Bearer issuer"
		if workload {
			expected = "Bearer minted"
			workloads++
			if objects["/api/v1/namespaces/preview-1"] == nil {
				t.Error("workload operation ran before namespace provisioning")
			}
		}
		if r.Header.Get("Authorization") != expected {
			t.Error("provisioning/workload credential boundary crossed")
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/apis/project.openshift.io/v1":
			w.WriteHeader(404)
			return
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"gitVersion": "v1.32.0"})
			return
		case "/api":
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "APIVersions", "apiVersion": "v1", "versions": []string{"v1"}})
			return
		case "/apis":
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}})
			return
		case "/api/v1":
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{map[string]any{"name": "configmaps", "kind": "ConfigMap", "namespaced": true, "verbs": []string{"get", "list", "create", "patch"}}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"token": "minted", "expirationTimestamp": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)}})
			return
		}
		if strings.Contains(r.URL.Path, "/configmaps") {
			if r.Method == "PATCH" {
				var object map[string]any
				_ = json.NewDecoder(r.Body).Decode(&object)
				_ = json.NewEncoder(w).Encode(object)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/configmaps") {
				_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "ConfigMapList", "items": []any{}})
				return
			}
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "Status", "apiVersion": "v1", "reason": "NotFound", "code": 404})
			return
		}
		if r.Method == "POST" {
			var object map[string]any
			_ = json.NewDecoder(r.Body).Decode(&object)
			metadata := object["metadata"].(map[string]any)
			name := metadata["name"].(string)
			metadata["uid"] = name + "-uid"
			metadata["resourceVersion"] = "1"
			objects[r.URL.Path+"/"+name] = object
			if object["kind"] == "Namespace" {
				namespaceCreates++
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(object)
			return
		}
		object := objects[r.URL.Path]
		if object == nil {
			w.WriteHeader(404)
			return
		}
		if r.Method == "DELETE" {
			deletes++
			delete(objects, r.URL.Path)
			w.WriteHeader(202)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	defer local.Close()
	key := []byte(strings.Repeat("k", 32))
	agent, err := clusteragent.New(clusteragent.Config{ClusterID: "fixture", ServerURL: "https://unused.invalid", IdentityFile: t.TempDir() + "/identity", EnrollmentTokenFile: "unused", Profiles: []agentprotocol.Profile{profile}}, &rest.Config{Host: local.URL, BearerToken: "issuer", TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: local.Certificate().Raw})}})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for runCtx.Err() == nil {
			id, cipher, claimErr := db.ClaimAgentTask(runCtx, "cluster", "test-lease")
			if claimErr != nil {
				if claimErr != sql.ErrNoRows && runCtx.Err() == nil {
					t.Error(claimErr)
				}
				select {
				case <-runCtx.Done():
					return
				case <-time.After(5 * time.Millisecond):
					continue
				}
			}
			plain, decryptErr := security.Decrypt(key, cipher, "agent-task:"+id+":request")
			if decryptErr != nil {
				t.Error(decryptErr)
				return
			}
			var task agentprotocol.Request
			if json.Unmarshal(plain, &task) != nil {
				t.Error("invalid queued task")
				return
			}
			task.Lease = "test-lease"
			result := agent.Execute(runCtx, task)
			raw, _ := json.Marshal(result)
			encrypted, encryptErr := security.Encrypt(key, raw, "agent-task:"+id+":response")
			if encryptErr != nil {
				t.Error(encryptErr)
				return
			}
			if completeErr := db.CompleteAgentTask(runCtx, "cluster", id, "test-lease", encrypted, result); completeErr != nil && runCtx.Err() == nil {
				t.Error(completeErr)
				return
			}
		}
	}()
	defer func() { cancel(); <-done }()
	svc := &Service{Store: db, EncryptionKey: key}
	input, err := svc.loadPlanInput(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	if !input.ManagedNamespaces["preview-1"] {
		t.Fatal("managed namespace was not resolved before discovery")
	}
	plan := core.Plan{}
	if err = addNamespaceCreations(ctx, input, &plan, nil); err != nil || len(plan.NamespaceCreations) != 0 {
		t.Fatal("managed namespace was sent through privileged generic namespace planning")
	}
	repo := newIntegrationGitRepo(t, "preview-1")
	if _, err = db.DB.ExecContext(ctx, `UPDATE git_sources SET repository_url=$1 WHERE id='source'`, repo.url); err != nil {
		t.Fatal(err)
	}
	calculated, _, err := svc.CalculatePlan(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(calculated.Changes) != 2 || len(calculated.NamespaceCreations) != 0 {
		t.Fatalf("expected two workload creates after provisioning, got %d", len(calculated.Changes))
	}
	environment, err := db.ManagedEnvironmentForNamespace(ctx, "cluster", "preview-1")
	if err != nil || environment.NamespaceUID != "preview-1-uid" || environment.Phase != "ready" {
		t.Fatal("namespace UID/readiness not persisted centrally")
	}
	// No workload was applied by planning. Cleanup enters deleting, returns pending,
	// and a later retry confirms namespace disappearance before app removal.
	if err = kube.CleanupApplicationNamespaces(ctx, db, key, app, false); err == nil || !strings.Contains(err.Error(), "finalizers") {
		t.Fatal("cleanup failed to expose pending namespace deletion")
	}
	if _, err = svc.loadPlanInput(ctx, app); err == nil {
		t.Fatal("deleting namespace accepted a new plan")
	}
	if err = kube.CleanupApplicationNamespaces(ctx, db, key, app, false); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DeleteApplicationKeepingResources(ctx, app.ID, true); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if namespaceCreates != 1 || deletes != 1 || workloads == 0 {
		t.Fatalf("unexpected lifecycle: namespace creates=%d deletes=%d workload requests=%d", namespaceCreates, deletes, workloads)
	}
}

func TestIntegrationManagedNamespaceDeletionGuards(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	app := managedNamespaceApplication("owner-app")
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	environment, err := db.RegisterManagedEnvironment(ctx, app, "preview-1")
	if err != nil {
		t.Fatal(err)
	}
	status := agentprotocol.EnvironmentStatus{NamespaceUID: "owned-uid", Phase: "ready", Managed: true}
	if err = db.SaveManagedEnvironmentStatus(ctx, environment.ID, status); err != nil {
		t.Fatal(err)
	}
	second := managedNamespaceApplication("shared-app")
	if err = db.CreateApplication(ctx, second); err != nil {
		t.Fatal(err)
	}
	environment, keep, err := db.BeginManagedEnvironmentDeletion(ctx, app, "preview-1", false)
	if err != nil || !keep {
		t.Fatal("another application did not prevent namespace deletion")
	}
	if _, err = db.DeleteApplicationKeepingResources(ctx, second.ID, true); err != nil {
		t.Fatal(err)
	}
	_, keep, err = db.BeginManagedEnvironmentDeletion(ctx, app, "preview-1", false)
	if err != nil || !keep {
		t.Fatal("shared namespace became exclusive after another app was removed")
	}
	// Use a separate destination to exercise deletion gates and concurrent app creation.
	exclusive := managedNamespaceApplication("exclusive-app")
	exclusive.Namespaces = []store.NamespaceBinding{{Namespace: "preview-2"}}
	if err = db.CreateApplication(ctx, exclusive); err != nil {
		t.Fatal(err)
	}
	exclusiveEnvironment, err := db.RegisterManagedEnvironment(ctx, exclusive, "preview-2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveManagedEnvironmentStatus(ctx, exclusiveEnvironment.ID, status); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, `INSERT INTO operations(id,application_id,status) VALUES('active-managed-operation',$1,'running')`, exclusive.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.BeginManagedEnvironmentDeletion(ctx, exclusive, "preview-2", false); err == nil {
		t.Fatal("active operation did not block namespace cleanup")
	}
	if _, err = db.DB.ExecContext(ctx, `UPDATE operations SET status='succeeded' WHERE id='active-managed-operation'`); err != nil {
		t.Fatal(err)
	}
	_, keep, err = db.BeginManagedEnvironmentDeletion(ctx, exclusive, "preview-2", false)
	if err != nil || keep {
		t.Fatal("exclusive deletion gate failed")
	}
	if err = db.CancelApplicationDecommission(ctx, exclusive.ID); err == nil {
		t.Fatal("decommission was canceled while namespace deletion was in progress")
	}
	if _, err = db.DeleteApplicationKeepingResources(ctx, exclusive.ID, false); err == nil {
		t.Fatal("orphan deletion removed an app with pending namespace cleanup")
	}
	conflicting := managedNamespaceApplication("new-app")
	conflicting.Namespaces = exclusive.Namespaces
	if err = db.CreateApplication(ctx, conflicting); err == nil {
		t.Fatal("application could join a deleting environment")
	}
	if _, err = db.DB.ExecContext(ctx, `INSERT INTO operations(id,application_id,status) VALUES('new-managed-operation',$1,'queued')`, exclusive.ID); err == nil {
		t.Fatal("deployment could start in a deleting environment")
	}
	if err = db.SaveManagedEnvironmentStatus(ctx, exclusiveEnvironment.ID, agentprotocol.EnvironmentStatus{NamespaceUID: "somebody-else", Phase: "deleted"}); err == nil {
		t.Fatal("cleanup accepted a changed namespace UID")
	}
}

func TestIntegrationManagedNamespaceCreationAndDeletionSerialize(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	for i := range 8 {
		owner := managedNamespaceApplication(fmt.Sprintf("race-owner-%d", i))
		owner.Namespaces = []store.NamespaceBinding{{Namespace: fmt.Sprintf("preview-%d", i+10)}}
		if err := db.CreateApplication(ctx, owner); err != nil {
			t.Fatal(err)
		}
		environment, err := db.RegisterManagedEnvironment(ctx, owner, owner.Namespaces[0].Namespace)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.SaveManagedEnvironmentStatus(ctx, environment.ID, agentprotocol.EnvironmentStatus{Phase: "ready", NamespaceUID: "race-uid", Managed: true}); err != nil {
			t.Fatal(err)
		}
		guest := owner
		guest.ID = fmt.Sprintf("race-guest-%d", i)
		guest.Name = guest.ID
		start := make(chan struct{})
		createResult := make(chan error, 1)
		type deletionResult struct {
			keep bool
			err  error
		}
		deleteResult := make(chan deletionResult, 1)
		go func() { <-start; createResult <- db.CreateApplication(ctx, guest) }()
		go func() {
			<-start
			_, keep, err := db.BeginManagedEnvironmentDeletion(ctx, owner, owner.Namespaces[0].Namespace, false)
			deleteResult <- deletionResult{keep, err}
		}()
		close(start)
		createErr := <-createResult
		deletion := <-deleteResult
		if deletion.err != nil {
			t.Fatal(deletion.err)
		}
		if createErr == nil && !deletion.keep {
			t.Fatal("concurrent application creation was missed by namespace cleanup")
		}
	}
}
