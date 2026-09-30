//go:build integration

package syncer

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
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIntegrationDeliveryAgainstKind(t *testing.T) {
	dsn, configPath := os.Getenv("JUSTCD_E2E_DATABASE_URL"), os.Getenv("JUSTCD_E2E_KUBECONFIG")
	if dsn == "" || configPath == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL and JUSTCD_E2E_KUBECONFIG for the real-cluster suite")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		t.Fatal(err)
	}
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	namespace := "justcd-e2e-" + store.NewID()[:8]
	if _, err := kubeClient.CoreV1().Namespaces().Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		_ = kubeClient.CoreV1().Namespaces().Delete(cleanupCtx, namespace, metav1.DeleteOptions{})
	})
	repo := newIntegrationGitRepo(t, namespace)
	key := bytes.Repeat([]byte{0x32}, 32)
	ownerID, workspaceID, credentialID, clusterID, sourceID := store.NewID(), store.NewID(), store.NewID(), store.NewID(), store.NewID()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email,display_name,is_admin) VALUES($1,$2,'Integration Owner',TRUE)`, ownerID, ownerID+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateWorkspace(ctx, store.Workspace{ID: workspaceID, Name: "Integration workspace"}, ownerID); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"content": string(content)})
	cipher, err := security.Encrypt(key, payload, "credential:"+credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCredential(ctx, store.Credential{ID: credentialID, WorkspaceID: &workspaceID, Name: "kind admin", Kind: "kubeconfig", Cipher: cipher}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCluster(ctx, store.Cluster{ID: clusterID, Name: "kind", APIServer: config.Host, CAData: config.CAData, DefaultCredentialID: &credentialID}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNamespaceBinding(ctx, workspaceID, clusterID, namespace, &credentialID); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateGitSource(ctx, store.GitSource{ID: sourceID, WorkspaceID: workspaceID, Name: "fixture", RepositoryURL: repo.url}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: db, EncryptionKey: key}
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		svc.RunOperationWorker(workerCtx, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	}()
	workerStopped := false
	defer func() {
		if !workerStopped {
			stopWorker()
			<-workerDone
		}
	}()

	for _, fixture := range []struct {
		name, renderer, path, resource string
		plannedResources               []string
	}{
		{"yaml", "yaml", "yaml", "yaml-agent", []string{"yaml-agent", "yaml-second"}},
		{"kustomize", "kustomize", "kustomize", "kustomize-agent", []string{"kustomize-agent"}},
		{"helm", "helm", "helm", "helm-agent", []string{"helm-agent"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			appID := store.NewID()
			app := store.Application{ID: appID, WorkspaceID: workspaceID, Name: fixture.name, SourceID: sourceID, Revision: "main", ManifestPath: fixture.path, Renderer: fixture.renderer, ClusterID: clusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
			if err := db.CreateApplication(ctx, app); err != nil {
				t.Fatal(err)
			}
			plan, err := svc.BuildPlan(ctx, appID, ownerID)
			if err != nil {
				t.Fatalf("build %s plan: %v", fixture.renderer, err)
			}
			if len(plan.Plan.Changes) != len(fixture.plannedResources) {
				t.Fatalf("plan contains %d changes, want %d", len(plan.Plan.Changes), len(fixture.plannedResources))
			}
			plannedNames := make(map[string]bool, len(plan.Plan.Changes))
			for _, change := range plan.Plan.Changes {
				if change.Kind != core.Create || change.Identity.Kind != "ConfigMap" || change.Identity.Namespace != namespace {
					t.Fatalf("unexpected rendered change: %+v", change)
				}
				plannedNames[change.Identity.Name] = true
			}
			for _, name := range fixture.plannedResources {
				if !plannedNames[name] {
					t.Fatalf("rendered plan omitted %s: %+v", name, plan.Plan.Changes)
				}
			}
			operation, err := svc.Apply(ctx, plan.ID, ownerID, "")
			if err != nil {
				t.Fatalf("queue %s: %v", fixture.renderer, err)
			}
			waitIntegrationOperation(t, ctx, db, operation.ID)
			if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, fixture.resource, metav1.GetOptions{}); err != nil {
				t.Fatalf("%s did not reach Kubernetes: %v", fixture.resource, err)
			}
			managed, err := db.ManagedResources(ctx, appID)
			if err != nil || len(managed) == 0 {
				t.Fatalf("managed inventory missing: %+v, %v", managed, err)
			}
			if fixture.renderer == "yaml" {
				verifyIntegrationReviewAndDrift(t, ctx, svc, db, kubeClient, repo, app, ownerID, namespace)
			}
		})
	}
	verifyIntegrationPartialFailure(t, ctx, svc, db, kubeClient, repo, workspaceID, clusterID, sourceID, credentialID, ownerID)
	verifyIntegrationConnectionFailures(t, ctx, svc, db, repo, workspaceID, clusterID, credentialID, ownerID, namespace)
	verifyIntegrationReadOnlyCredential(t, ctx, svc, db, kubeClient, repo, key, workspaceID, sourceID, ownerID, namespace, config.Host, config.CAData)
	stopWorker()
	<-workerDone
	workerStopped = true
	verifyIntegrationWorkerCrash(t, ctx, svc, db, repo, workspaceID, clusterID, sourceID, credentialID, ownerID, namespace)
	audit, err := db.ListAuditEvents(ctx, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	observed := map[string]bool{}
	for _, item := range audit {
		observed[item.Action] = true
	}
	for _, action := range []string{"plan.created", "sync.succeeded", "sync.failed", "plan.approved", "rollback.plan_created", "rollback.succeeded", "application.decommission_plan_created"} {
		if !observed[action] {
			t.Errorf("audit event %s missing", action)
		}
	}
}

func verifyIntegrationPartialFailure(t *testing.T, ctx context.Context, svc *Service, db *store.Store, kubeClient *kubernetes.Clientset, repo *integrationGitRepo, workspaceID, clusterID, sourceID, credentialID, ownerID string) {
	t.Helper()
	namespace := "justcd-e2e-partial-" + store.NewID()[:8]
	if _, err := kubeClient.CoreV1().Namespaces().Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		_ = kubeClient.CoreV1().Namespaces().Delete(cleanupCtx, namespace, metav1.DeleteOptions{})
	})
	for {
		if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "kube-root-ca.crt", metav1.GetOptions{}); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("namespace did not receive its root CA ConfigMap")
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Kubernetes creates kube-root-ca.crt in each namespace, leaving room for
	// exactly one application ConfigMap under this quota.
	quota := &v1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "two-configmaps"}, Spec: v1.ResourceQuotaSpec{Hard: v1.ResourceList{v1.ResourceName("count/configmaps"): resource.MustParse("2")}}}
	if _, err := kubeClient.CoreV1().ResourceQuotas(namespace).Create(ctx, quota, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNamespaceBinding(ctx, workspaceID, clusterID, namespace, &credentialID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a-partial", "z-partial"} {
		repo.write(t, "partial/"+name+".yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: %s\ndata:\n  mode: desired\n", name, namespace))
	}
	repo.commit(t, "add partial failure fixture")
	appID := store.NewID()
	app := store.Application{ID: appID, WorkspaceID: workspaceID, Name: "partial failure", SourceID: sourceID, Revision: "main", ManifestPath: "partial", Renderer: "yaml", ClusterID: clusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.BuildPlan(ctx, appID, ownerID)
	if err != nil || len(plan.Plan.Changes) != 2 {
		t.Fatalf("partial fixture plan: %d changes, %v", len(plan.Plan.Changes), err)
	}
	op, err := svc.Apply(ctx, plan.ID, ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	failed := waitIntegrationOperationStatus(t, ctx, db, op.ID, "failed")
	if len(failed.Progress.Completed) != 1 || failed.ErrorCode == "" {
		t.Fatalf("partial failure lost progress or error: %+v", failed.Progress)
	}
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "a-partial", metav1.GetOptions{}); err != nil {
		t.Fatalf("first resource was not applied: %v", err)
	}
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "z-partial", metav1.GetOptions{}); err == nil {
		t.Fatal("quota-rejected second resource was applied")
	}
}

func verifyIntegrationConnectionFailures(t *testing.T, ctx context.Context, svc *Service, db *store.Store, repo *integrationGitRepo, workspaceID, clusterID, credentialID, ownerID, namespace string) {
	t.Helper()
	badSourceID := store.NewID()
	if err := db.CreateGitSource(ctx, store.GitSource{ID: badSourceID, WorkspaceID: workspaceID, Name: "offline source", RepositoryURL: "https://127.0.0.1:1/unreachable.git"}); err != nil {
		t.Fatal(err)
	}
	gitAppID := store.NewID()
	gitApp := store.Application{ID: gitAppID, WorkspaceID: workspaceID, Name: "offline Git", SourceID: badSourceID, Revision: "main", ManifestPath: "yaml", Renderer: "yaml", ClusterID: clusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
	if err := db.CreateApplication(ctx, gitApp); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BuildPlan(ctx, gitAppID, ownerID); err == nil {
		t.Fatal("offline Git source unexpectedly produced a plan")
	}
	got, err := db.ApplicationByID(ctx, gitAppID)
	if err != nil || len(got.StatusIssues) == 0 || got.StatusIssues[0].Source != "git" {
		t.Fatalf("Git failure did not reach application status: %+v, %v", got.StatusIssues, err)
	}
	badClusterID := store.NewID()
	if err := db.CreateCluster(ctx, store.Cluster{ID: badClusterID, Name: "offline cluster", APIServer: "https://127.0.0.1:1", CAData: []byte{}, DefaultCredentialID: &credentialID}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNamespaceBinding(ctx, workspaceID, badClusterID, namespace, &credentialID); err != nil {
		t.Fatal(err)
	}
	clusterAppID := store.NewID()
	clusterApp := store.Application{ID: clusterAppID, WorkspaceID: workspaceID, Name: "offline cluster", SourceID: gitSourceID(t, ctx, db, workspaceID, repo.url), Revision: "main", ManifestPath: "kustomize", Renderer: "kustomize", ClusterID: badClusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
	if err := db.CreateApplication(ctx, clusterApp); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BuildPlan(ctx, clusterAppID, ownerID); err == nil {
		t.Fatal("offline cluster unexpectedly produced a plan")
	}
	got, err = db.ApplicationByID(ctx, clusterAppID)
	if err != nil || len(got.StatusIssues) == 0 || got.StatusIssues[0].Source != "cluster" {
		t.Fatalf("cluster failure did not reach application status: %+v, %v", got.StatusIssues, err)
	}
}

func gitSourceID(t *testing.T, ctx context.Context, db *store.Store, workspaceID, url string) string {
	t.Helper()
	var id string
	if err := db.DB.QueryRowContext(ctx, `SELECT id FROM git_sources WHERE workspace_id=$1 AND repository_url=$2`, workspaceID, url).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func verifyIntegrationReadOnlyCredential(t *testing.T, ctx context.Context, svc *Service, db *store.Store, kubeClient *kubernetes.Clientset, repo *integrationGitRepo, key []byte, workspaceID, sourceID, ownerID, namespace, apiServer string, caData []byte) {
	t.Helper()
	serviceAccount := "justcd-e2e-reader"
	if _, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(ctx, &v1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccount}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: serviceAccount}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "watch"}}}}
	if _, err := kubeClient.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: serviceAccount}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: serviceAccount, Namespace: namespace}}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: serviceAccount}}
	if _, err := kubeClient.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	token, err := kubeClient.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, serviceAccount, &authenticationv1.TokenRequest{}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	credentialID, clusterID := store.NewID(), store.NewID()
	payload, _ := json.Marshal(map[string]string{"token": token.Status.Token})
	cipher, err := security.Encrypt(key, payload, "credential:"+credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCredential(ctx, store.Credential{ID: credentialID, WorkspaceID: &workspaceID, Name: "read-only token", Kind: "kubernetes-token", Cipher: cipher}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCluster(ctx, store.Cluster{ID: clusterID, Name: "kind read only", APIServer: apiServer, CAData: caData, DefaultCredentialID: &credentialID}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNamespaceBinding(ctx, workspaceID, clusterID, namespace, &credentialID); err != nil {
		t.Fatal(err)
	}
	repo.write(t, "read-only/agent.yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: read-only-agent\n  namespace: %s\ndata:\n  mode: desired\n", namespace))
	repo.commit(t, "add read-only fixture")
	appID := store.NewID()
	app := store.Application{ID: appID, WorkspaceID: workspaceID, Name: "read-only target", SourceID: sourceID, Revision: "main", ManifestPath: "read-only", Renderer: "yaml", ClusterID: clusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.BuildPlan(ctx, appID, ownerID)
	if err != nil {
		t.Fatalf("read-only credential should permit planning: %v", err)
	}
	op, err := svc.Apply(ctx, plan.ID, ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	failed := waitIntegrationOperationStatus(t, ctx, db, op.ID, "failed")
	if failed.ErrorCode == "" {
		t.Fatal("permission denial has no failure code")
	}
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "read-only-agent", metav1.GetOptions{}); err == nil {
		t.Fatal("read-only token unexpectedly created a ConfigMap")
	}
}

func verifyIntegrationWorkerCrash(t *testing.T, ctx context.Context, svc *Service, db *store.Store, repo *integrationGitRepo, workspaceID, clusterID, sourceID, credentialID, ownerID, namespace string) {
	t.Helper()
	repo.write(t, "crash/agent.yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: crash-agent\n  namespace: %s\ndata:\n  mode: desired\n", namespace))
	repo.commit(t, "add worker interruption fixture")
	appID := store.NewID()
	app := store.Application{ID: appID, WorkspaceID: workspaceID, Name: "worker crash", SourceID: sourceID, Revision: "main", ManifestPath: "crash", Renderer: "yaml", ClusterID: clusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.BuildPlan(ctx, appID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := svc.Apply(ctx, plan.ID, ownerID, "")
	if err != nil {
		t.Fatal(err)
	}
	// The previous operation may still hold the cluster rate-limit gate.
	// Wait for scheduler eligibility instead of assuming an immediate claim.
	claimCtx, cancelClaim := context.WithTimeout(ctx, 15*time.Second)
	defer cancelClaim()
	for {
		claimed, ok, err := db.ClaimQueuedOperation(claimCtx, 90*time.Second)
		if err != nil {
			t.Fatalf("claim crash fixture: %v", err)
		}
		if ok {
			if claimed.ID != queued.ID {
				t.Fatalf("claimed unexpected operation %s, want %s", claimed.ID, queued.ID)
			}
			break
		}
		select {
		case <-claimCtx.Done():
			t.Fatalf("crash fixture did not become eligible: %v", claimCtx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE operation_leases SET expires_at=NOW()-INTERVAL '1 second' WHERE operation_id=$1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverInterruptedOperations(ctx); err != nil {
		t.Fatal(err)
	}
	failed, err := db.OperationByID(ctx, queued.ID)
	if err != nil || failed.Status != "failed" || failed.ErrorCode != "operation.interrupted" {
		t.Fatalf("worker crash not recovered: %+v, %v", failed, err)
	}
}

func verifyIntegrationReviewAndDrift(t *testing.T, ctx context.Context, svc *Service, db *store.Store, kubeClient *kubernetes.Clientset, repo *integrationGitRepo, app store.Application, ownerID, namespace string) {
	t.Helper()
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "yaml-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cm.Data["mode"] = "drifted"
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	drift, err := svc.BuildPlan(ctx, app.ID, ownerID)
	if err != nil || len(drift.Plan.Changes) == 0 {
		t.Fatalf("external drift was not planned: %+v, %v", drift.Plan.Changes, err)
	}
	var takeover bool
	for _, change := range drift.Plan.Changes {
		takeover = takeover || change.Takeover
	}
	if !takeover || !drift.Plan.RequiresApproval {
		t.Fatal("external field ownership did not require a reviewed takeover")
	}
	if _, err := svc.Apply(ctx, drift.ID, ownerID, ""); err == nil {
		t.Fatal("unapproved drift takeover was queued")
	}
	driftApproval := createIntegrationApproval(t, ctx, db, drift, ownerID)
	op, err := svc.ApplyWithApprovals(ctx, drift.ID, ownerID, []string{driftApproval})
	if err != nil {
		t.Fatalf("queue reviewed drift takeover: %v", err)
	}
	waitIntegrationOperation(t, ctx, db, op.ID)
	cm, err = kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "yaml-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cm.Data["mode"] != "desired" {
		t.Fatalf("approved takeover did not restore desired data: %+v", cm.Data)
	}
	repo.write(t, "yaml/second.yaml", "")
	repo.commit(t, "remove second ConfigMap")
	deletion, err := svc.BuildPlan(ctx, app.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if !deletion.Plan.RequiresApproval {
		t.Fatal("deletion plan did not require approval")
	}
	var deletes []core.Change
	for _, change := range deletion.Plan.Changes {
		if change.Kind == core.Delete {
			deletes = append(deletes, change)
		}
	}
	if len(deletes) != 1 {
		t.Fatalf("expected one deletion, got %d", len(deletes))
	}
	if _, err := svc.Apply(ctx, deletion.ID, ownerID, ""); err == nil {
		t.Fatal("unapproved deletion was queued")
	}
	approvalID := store.NewID()
	approval := core.DeletionApproval{ActorID: ownerID, PlanDigest: deletion.Plan.Digest, Deletes: deletes, ExpiresAt: deletion.ExpiresAt}
	if err := db.CreateApproval(ctx, approvalID, deletion.ID, approval, deletion.ExpiresAt, "integration review"); err != nil {
		t.Fatal(err)
	}
	_ = db.Audit(ctx, ownerID, "plan.approved", "application", app.ID, map[string]any{"planId": deletion.ID, "approvalId": approvalID})
	op, err = svc.ApplyWithApprovals(ctx, deletion.ID, ownerID, []string{approvalID})
	if err != nil {
		t.Fatal(err)
	}
	waitIntegrationOperation(t, ctx, db, op.ID)
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "yaml-second", metav1.GetOptions{}); err == nil {
		t.Fatal("deleted ConfigMap still exists")
	}
	targets, err := svc.RollbackTargets(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	var originalTarget string
	for _, target := range targets {
		if target.Kind == "successful_sync" && target.ResourceCount == 2 {
			originalTarget = target.ID
			break
		}
	}
	if originalTarget == "" {
		t.Fatal("the original successful deployment has no rollback snapshot")
	}
	rollback, err := svc.BuildRollbackPlan(ctx, app, ownerID, "successful_sync", originalTarget, "")
	if err != nil {
		t.Fatal(err)
	}
	storedRollback, err := db.PlanByID(ctx, rollback.ID)
	if err != nil {
		t.Fatal(err)
	}
	recomputed := storedRollback.Plan
	if err := core.RefreshDigest(&recomputed); err != nil {
		t.Fatal(err)
	}
	if recomputed.Digest != rollback.Plan.Digest {
		t.Fatal("rollback digest changed after storage")
	}
	rollbackApproval := createIntegrationApproval(t, ctx, db, rollback, ownerID)
	op, err = svc.ApplyWithApprovals(ctx, rollback.ID, ownerID, []string{rollbackApproval})
	if err != nil {
		t.Fatalf("queue rollback: %v", err)
	}
	waitIntegrationOperation(t, ctx, db, op.ID)
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "yaml-second", metav1.GetOptions{}); err != nil {
		t.Fatalf("rollback did not restore the ConfigMap: %v", err)
	}
	currentApp, err := db.ApplicationByID(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	decommission, err := svc.BuildDecommissionPlan(ctx, currentApp, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	decommissionApproval := createIntegrationApproval(t, ctx, db, decommission, ownerID)
	op, err = svc.ApplyWithApprovals(ctx, decommission.ID, ownerID, []string{decommissionApproval})
	if err != nil {
		t.Fatalf("queue decommission: %v", err)
	}
	waitIntegrationOperation(t, ctx, db, op.ID)
	if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, "yaml-agent", metav1.GetOptions{}); err == nil {
		t.Fatal("decommission left a managed ConfigMap behind")
	}
}

func createIntegrationApproval(t *testing.T, ctx context.Context, db *store.Store, plan store.PlanRecord, ownerID string) string {
	t.Helper()
	var deletes, privileged []core.Change
	for _, change := range plan.Plan.Changes {
		if change.Kind == core.Delete {
			deletes = append(deletes, change)
		}
		if change.Identity.ClusterScoped {
			privileged = append(privileged, change)
		}
	}
	id := store.NewID()
	approval := core.DeletionApproval{ActorID: ownerID, PlanDigest: plan.Plan.Digest, Deletes: deletes, Privileged: privileged, ExpiresAt: plan.ExpiresAt}
	if err := db.CreateApproval(ctx, id, plan.ID, approval, plan.ExpiresAt, "integration review"); err != nil {
		t.Fatal(err)
	}
	return id
}

func waitIntegrationOperation(t *testing.T, ctx context.Context, db *store.Store, id string) {
	t.Helper()
	_ = waitIntegrationOperationStatus(t, ctx, db, id, "succeeded")
}

func waitIntegrationOperationStatus(t *testing.T, ctx context.Context, db *store.Store, id, want string) store.Operation {
	t.Helper()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		op, err := db.OperationByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if op.Status == want {
			return op
		}
		if op.Status == "failed" || op.Status == "cancelled" || op.Status == "succeeded" {
			t.Fatalf("operation %s: %s: %s", id, op.Status, op.Message)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("operation %s timed out: %v", id, ctx.Err())
		case <-ticker.C:
		}
	}
}

type integrationGitRepo struct{ root, work, url string }

func newIntegrationGitRepo(t *testing.T, namespace string) *integrationGitRepo {
	t.Helper()
	root := t.TempDir()
	repo := &integrationGitRepo{root: root, work: filepath.Join(root, "work")}
	integrationGit(t, root, "init", "--bare", filepath.Join(root, "repo.git"))
	integrationGit(t, root, "init", repo.work)
	integrationGit(t, repo.work, "config", "user.name", "Integration")
	integrationGit(t, repo.work, "config", "user.email", "integration@example.invalid")
	integrationGit(t, repo.work, "remote", "add", "origin", filepath.Join(root, "repo.git"))
	repo.write(t, "yaml/agent.yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: yaml-agent\n  namespace: %s\ndata:\n  mode: desired\n", namespace))
	repo.write(t, "yaml/second.yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: yaml-second\n  namespace: %s\ndata:\n  mode: desired\n", namespace))
	repo.write(t, "kustomize/kustomization.yaml", "resources:\n  - agent.yaml\n")
	repo.write(t, "kustomize/agent.yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: kustomize-agent\n  namespace: %s\ndata:\n  mode: desired\n", namespace))
	repo.write(t, "helm/Chart.yaml", "apiVersion: v2\nname: integration\nversion: 0.1.0\n")
	repo.write(t, "helm/templates/agent.yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: helm-agent\n  namespace: %s\ndata:\n  mode: {{ .Values.mode | quote }}\n", namespace))
	repo.write(t, "helm/values.yaml", "mode: desired\n")
	repo.commit(t, "initial fixtures")
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cmd := exec.Command("git", "http-backend")
		cmd.Env = append(os.Environ(), "GIT_PROJECT_ROOT="+root, "GIT_HTTP_EXPORT_ALL=1", "PATH_INFO="+r.URL.Path, "QUERY_STRING="+r.URL.RawQuery, "REQUEST_METHOD="+r.Method, "CONTENT_TYPE="+r.Header.Get("Content-Type"), fmt.Sprintf("CONTENT_LENGTH=%d", r.ContentLength))
		cmd.Stdin = r.Body
		output, err := cmd.Output()
		if err != nil {
			http.Error(w, "Git fixture unavailable", http.StatusBadGateway)
			return
		}
		parts := bytes.SplitN(output, []byte("\r\n\r\n"), 2)
		if len(parts) != 2 {
			http.Error(w, "Invalid Git fixture response", http.StatusBadGateway)
			return
		}
		for _, line := range strings.Split(string(parts[0]), "\r\n") {
			key, value, ok := strings.Cut(line, ":")
			if ok {
				w.Header().Set(key, strings.TrimSpace(value))
			}
		}
		_, _ = w.Write(parts[1])
	}))
	server.StartTLS()
	t.Cleanup(server.Close)
	certFile := filepath.Join(root, "git-ca.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSL_CAINFO", certFile)
	repo.url = server.URL + "/repo.git"
	return repo
}

func (r *integrationGitRepo) write(t *testing.T, path, content string) {
	t.Helper()
	full := filepath.Join(r.work, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if content == "" {
		if err := os.Remove(full); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func (r *integrationGitRepo) commit(t *testing.T, message string) {
	t.Helper()
	integrationGit(t, r.work, "add", "-A")
	integrationGit(t, r.work, "commit", "-m", message)
	integrationGit(t, r.work, "push", "origin", "HEAD:refs/heads/main")
}

func integrationGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
