//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/justlab/justcd/services/backend/internal/syncer"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIntegrationBackgroundTasks(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('task-owner','task@example.invalid'),('task-other','other@example.invalid');
 INSERT INTO workspaces(id,name,approval_policy) VALUES('task-workspace','Tasks','{"sync":{"requiredApprovals":1,"approverRoles":["owner"],"approverUserIds":[]},"deletion":{"requiredApprovals":1,"approverRoles":["owner"],"approverUserIds":[]}}');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('task-workspace','task-owner','owner'),('task-workspace','task-other','owner');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('task-source','task-workspace','Git','https://example.invalid/repo.git');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('task-cluster','task-workspace','Cluster','https://example.invalid');
 INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy) VALUES('task-app','task-workspace','App','task-source','main','deploy','yaml','task-cluster','[{"namespace":"dev"}]','manual')`)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	s := &Server{Store: db, Logger: slog.Default(), EncryptionKey: key, Syncer: &syncer.Service{Store: db, EncryptionKey: key}}
	request := func(handler http.HandlerFunc, actor, key, pathID, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		r.SetPathValue("applicationID", pathID)
		r.SetPathValue("planID", pathID)
		r.SetPathValue("taskID", pathID)
		r = r.WithContext(context.WithValue(ctx, userContextKey, store.User{ID: actor}))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	enqueue := func(handler http.HandlerFunc, key, pathID, body string) store.BackgroundTask {
		w := request(handler, "task-owner", key, pathID, body)
		if w.Code != 202 {
			t.Fatalf("queue: %d %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Location") == "" {
			t.Fatal("queue response missing status URL")
		}
		var task store.BackgroundTask
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		return task
	}

	first := enqueue(s.createPlan, "plan-request", "task-app", `{}`)
	var wg sync.WaitGroup
	ids := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task, err := db.EnqueueBackgroundTask(ctx, store.BackgroundTask{ApplicationID: "task-app", ActorID: "task-owner", Kind: "plan", RequestID: fmt.Sprintf("concurrent-%d", i), Input: json.RawMessage(`{}`)})
			if err != nil {
				t.Error(err)
			}
			ids <- task.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		if id != first.ID {
			t.Fatal("concurrent submission created duplicate work")
		}
	}
	if w := request(s.getBackgroundTask, "task-other", "", first.ID, ""); w.Code != 404 {
		t.Fatal("another actor read private task")
	}
	claimed, ok, err := db.ClaimBackgroundTask(ctx, backgroundTaskLease)
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	if _, ok, err := db.ClaimBackgroundTask(ctx, backgroundTaskLease); err != nil || ok {
		t.Fatalf("task claimed twice: %v %v", ok, err)
	}
	if err := db.FinishBackgroundTask(ctx, claimed, 201, json.RawMessage(`{"id":"test-plan"}`)); err != nil {
		t.Fatal(err)
	}
	alias := enqueue(s.createPlan, "concurrent-0", "task-app", `{}`)
	if alias.ID != first.ID || alias.Status != "succeeded" {
		t.Fatal("retry after completion did not reuse original task")
	}
	mismatch := claimed
	mismatch.RequestID = "plan-request"
	mismatch.Input = json.RawMessage(`{"different":true}`)
	if _, err := db.EnqueueBackgroundTask(ctx, mismatch); err != store.ErrTaskRequestMismatch {
		t.Fatalf("mismatched idempotency key: %v", err)
	}

	interrupted := enqueue(s.createPlan, "interrupted", "task-app", `{}`)
	claimed, _, err = db.ClaimBackgroundTask(ctx, backgroundTaskLease)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE background_tasks SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, interrupted.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverBackgroundTasks(ctx, interruptedTaskResponse().Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBackgroundTask(ctx, claimed, 201, json.RawMessage(`{}`)); err == nil {
		t.Fatal("stale worker overwrote recovery")
	}
	if _, ok, err := db.ClaimBackgroundTask(ctx, backgroundTaskLease); err != nil || ok {
		t.Fatal("interrupted work was replayed")
	}

	// Run actual planning and approval logic against local Git and read-only fake
	// Kubernetes. Block discovery to prove the submit request never waits for it.
	repoURL := taskGitFixture(t)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var once sync.Once
	var liveVersion atomic.Int64
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/version" {
			once.Do(func() { started <- struct{}{} })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"major":"1","minor":"30","gitVersion":"v1.30.0"}`)
			return
		}
		switch r.URL.Path {
		case "/api":
			fmt.Fprint(w, `{"kind":"APIVersions","versions":["v1"]}`)
		case "/apis":
			fmt.Fprint(w, `{"kind":"APIGroupList","groups":[]}`)
		case "/api/v1":
			fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"configmaps","kind":"ConfigMap","namespaced":true,"verbs":["get","list","create","patch"]}]}`)
		default:
			version := liveVersion.Load()
			object := fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"remaining","namespace":"dev","uid":"remaining-uid","resourceVersion":"%d","labels":{"justcd.io/application-id":"task-app"}},"data":{"value":"keep"}}`, version)
			if strings.HasSuffix(r.URL.Path, "/configmaps") {
				if version > 0 {
					fmt.Fprintf(w, `{"apiVersion":"v1","kind":"ConfigMapList","items":[%s]}`, object)
				} else {
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"ConfigMapList","items":[]}`)
				}
			} else if strings.HasSuffix(r.URL.Path, "/configmaps/remaining") && version > 0 {
				fmt.Fprint(w, object)
			} else {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"apiVersion":"v1","kind":"Status","reason":"NotFound","code":404}`)
			}
		}
	}))
	defer kube.Close()
	cipher, err := security.Encrypt(key, []byte(`{"token":"fixture"}`), "credential:task-credential")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES('task-credential','task-workspace','Kube','kubernetes-token',$1)`, cipher); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, `UPDATE git_sources SET repository_url=$1 WHERE id='task-source';`, repoURL); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, `UPDATE clusters SET api_server=$1,default_credential_id='task-credential' WHERE id='task-cluster'`, kube.URL); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, `INSERT INTO namespace_bindings(id,workspace_id,cluster_id,namespace,credential_id) VALUES('task-binding','task-workspace','task-cluster','dev','task-credential')`); err != nil {
		t.Fatal(err)
	}
	next := enqueue(s.createPlan, "actual-plan", "task-app", `{}`)
	acquire, cancelAcquire := context.WithCancel(ctx)
	execute, cancelExecute := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); s.RunTaskWorker(acquire, execute, slog.Default()) }()
	defer func() { cancelAcquire(); cancelExecute(); <-workerDone }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("planning never reached discovery")
	}
	retry := enqueue(s.createPlan, "actual-plan-retry", "task-app", `{}`)
	if retry.ID != next.ID {
		t.Fatal("running plan duplicated")
	}
	close(release)
	completed := waitTaskFixture(t, db, next.ID)
	if completed.Status != "succeeded" {
		t.Fatalf("plan failed: %s", completed.Result)
	}
	var plan planView
	if err = json.Unmarshal(completed.Result, &plan); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"planDigest":%q,"comment":"Reviewed"}`, plan.Plan.Digest)
	approval := enqueue(s.approvePlan, "actual-approval", plan.ID, body)
	completed = waitTaskFixture(t, db, approval.ID)
	if completed.Status != "succeeded" {
		t.Fatalf("approval failed: %s", completed.Result)
	}
	approved, err := db.ListPlanApprovals(ctx, plan.ID, plan.Plan.Digest)
	if err != nil || len(approved) != 1 {
		t.Fatalf("approval count: %d %v", len(approved), err)
	}
	if again := enqueue(s.approvePlan, "actual-approval", plan.ID, body); again.ID != approval.ID {
		t.Fatal("approval retry duplicated work")
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE plans SET expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, plan.ID); err != nil {
		t.Fatal(err)
	}
	stale := enqueue(s.approvePlan, "stale-approval", plan.ID, body)
	completed = waitTaskFixture(t, db, stale.ID)
	var failure struct {
		Code string   `json:"code"`
		Plan planView `json:"plan"`
	}
	if err := json.Unmarshal(completed.Result, &failure); err != nil {
		t.Fatal(err)
	}
	if completed.Status != "failed" || failure.Code != "plan.stale" || failure.Plan.ID == "" {
		t.Fatalf("stale approval lost refreshed plan: %s", completed.Result)
	}
	var approvalCount int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM deletion_approvals`).Scan(&approvalCount); err != nil || approvalCount != 1 {
		t.Fatalf("stale plan recorded approval: %d %v", approvalCount, err)
	}

	// Refresh during deletion must read remaining live resources, even when the
	// previous deletion plan is current and unexpired. Never build a Git sync.
	liveVersion.Store(1)
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON([]byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"remaining","namespace":"dev","uid":"remaining-uid","resourceVersion":"1","labels":{"justcd.io/application-id":"task-app"}},"data":{"value":"keep"}}`)); err != nil {
		t.Fatal(err)
	}
	resource, err := render.CanonicalLive(object, "task-cluster", "task-app", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertManagedResource(ctx, "task-app", resource); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET decommissioning=TRUE,configuration_missing=TRUE WHERE id='task-app'`); err != nil {
		t.Fatal(err)
	}
	refreshDeletion := func(key string) planView {
		t.Helper()
		task := enqueue(s.createPlan, key, "task-app", `{}`)
		outcome := waitTaskFixture(t, db, task.ID)
		var view planView
		if outcome.Status != "succeeded" {
			t.Fatalf("deletion refresh failed: %s", outcome.Result)
		}
		if err := json.Unmarshal(outcome.Result, &view); err != nil {
			t.Fatal(err)
		}
		if !view.Plan.Decommission {
			t.Fatal("deletion refresh created a normal sync plan")
		}
		return view
	}
	deletion := refreshDeletion("deletion-refresh")
	if len(deletion.Plan.Changes) != 1 {
		t.Fatalf("expected remaining resource, got %+v", deletion.Plan.Changes)
	}
	liveVersion.Store(2)
	refreshed := refreshDeletion("deletion-version-refresh")
	if refreshed.ID == deletion.ID || refreshed.Plan.Changes[0].LiveResourceVersion != "2" {
		t.Fatal("refresh reused a stale unexpired deletion plan")
	}
	liveVersion.Store(0)
	remaining := refreshDeletion("deletion-partial-refresh")
	if len(remaining.Plan.Changes) != 0 {
		t.Fatal("refresh retained a resource already deleted by a partial operation")
	}

	// Authorization changes after enqueue must be checked by the worker.
	cancelAcquire()
	cancelExecute()
	<-workerDone
	workerDone = make(chan struct{})
	close(workerDone)
	revoked := enqueue(s.approvePlan, "revoked", plan.ID, body)
	if _, err := db.DB.ExecContext(ctx, `DELETE FROM workspace_memberships WHERE user_id='task-owner'`); err != nil {
		t.Fatal(err)
	}
	claimed, _, err = db.ClaimBackgroundTask(ctx, backgroundTaskLease)
	if err != nil || claimed.ID != revoked.ID {
		t.Fatal(err)
	}
	s.runBackgroundTask(ctx, claimed, slog.Default())
	completed = waitTaskFixture(t, db, revoked.ID)
	if completed.HTTPStatus == nil || *completed.HTTPStatus != 403 {
		t.Fatalf("revoked access not enforced: %s", completed.Result)
	}
}

func waitTaskFixture(t *testing.T, db *store.Store, id string) store.BackgroundTask {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		task, err := db.BackgroundTaskByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == "succeeded" || task.Status == "failed" {
			return task
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return store.BackgroundTask{}
}

func taskGitFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo.git")
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	git("init", "--initial-branch=main", repo)
	git("-C", repo, "config", "user.name", "Task test")
	git("-C", repo, "config", "user.email", "task@example.invalid")
	if err := os.Mkdir(filepath.Join(repo, "deploy"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "deploy", "config.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: task-config\n  namespace: dev\ndata:\n  value: test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("-C", repo, "add", ".")
	git("-C", repo, "commit", "-m", "fixture")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cmd := exec.CommandContext(r.Context(), "git", "http-backend")
		cmd.Env = append(os.Environ(), "GIT_PROJECT_ROOT="+root, "GIT_HTTP_EXPORT_ALL=1", "PATH_INFO="+r.URL.Path, "QUERY_STRING="+r.URL.RawQuery, "REQUEST_METHOD="+r.Method, "CONTENT_TYPE="+r.Header.Get("Content-Type"), fmt.Sprintf("CONTENT_LENGTH=%d", r.ContentLength))
		cmd.Stdin = r.Body
		output, err := cmd.Output()
		if err != nil {
			http.Error(w, "fixture unavailable", 502)
			return
		}
		parts := bytes.SplitN(output, []byte("\r\n\r\n"), 2)
		if len(parts) != 2 {
			http.Error(w, "fixture unavailable", 502)
			return
		}
		for _, line := range strings.Split(string(parts[0]), "\r\n") {
			k, v, ok := strings.Cut(line, ":")
			if ok {
				w.Header().Set(k, strings.TrimSpace(v))
			}
		}
		w.Write(parts[1])
	}))
	t.Cleanup(server.Close)
	cert := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSL_CAINFO", cert)
	return server.URL + "/repo.git"
}
