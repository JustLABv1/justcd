//go:build integration

package syncer

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Spawned from the already-built integration test binary and killed with SIGKILL
// at a durable boundary. No defers or graceful-shutdown cleanup run in the child.
func TestIntegrationCrashWorkerProcess(t *testing.T) {
	if os.Getenv("JUSTCD_CRASH_TEST_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := store.Open(ctx, os.Getenv("JUSTCD_E2E_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	key, err := hex.DecodeString(os.Getenv("JUSTCD_CRASH_TEST_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: db, EncryptionKey: key, testCheckpoint: func(stage string) {
		if stage == os.Getenv("JUSTCD_CRASH_TEST_STAGE") {
			if err := os.WriteFile(os.Getenv("JUSTCD_CRASH_TEST_MARKER"), []byte(stage), 0600); err != nil {
				t.Fatal(err)
			}
			<-ctx.Done()
		}
	}}
	if os.Getenv("JUSTCD_CRASH_TEST_MODE") == "plan" {
		_, err := svc.BuildPlan(ctx, os.Getenv("JUSTCD_CRASH_TEST_APP"), os.Getenv("JUSTCD_CRASH_TEST_OWNER"))
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal("planner did not stop at checkpoint")
	}
	svc.RunOperationWorker(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Fatal("worker exited before process kill")
}

func killWorkerAtCheckpoint(t *testing.T, ctx context.Context, key []byte, appID, ownerID, stage, mode string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "checkpoint")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIntegrationCrashWorkerProcess$")
	cmd.Env = append(os.Environ(), "JUSTCD_CRASH_TEST_CHILD=1", "JUSTCD_CRASH_TEST_KEY="+hex.EncodeToString(key), "JUSTCD_CRASH_TEST_APP="+appID, "JUSTCD_CRASH_TEST_OWNER="+ownerID, "JUSTCD_CRASH_TEST_STAGE="+stage, "JUSTCD_CRASH_TEST_MODE="+mode, "JUSTCD_CRASH_TEST_MARKER="+marker)
	output, err := os.Create(filepath.Join(t.TempDir(), "child.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	killed := false
	defer func() {
		if !killed {
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	deadline, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case err := <-done:
			killed = true
			data, _ := os.ReadFile(output.Name())
			t.Fatalf("child exited before %s: %v\n%s", stage, err, data)
		case <-deadline.Done():
			data, _ := os.ReadFile(output.Name())
			t.Fatalf("checkpoint %s timed out\n%s", stage, data)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-done
	killed = true
}

func executeCrashFixture(t *testing.T, ctx context.Context, svc *Service, app store.Application, plan store.PlanRecord, ownerID string) store.Operation {
	t.Helper()
	approvalIDs := []string{}
	if plan.Plan.RequiresApproval {
		approvalIDs = append(approvalIDs, createIntegrationApproval(t, ctx, svc.Store, plan, ownerID))
	}
	op, err := svc.ApplyWithApprovals(ctx, plan.ID, ownerID, approvalIDs)
	if err != nil {
		t.Fatal(err)
	}
	resetCrashGate(t, ctx, svc.Store, app.ClusterID)
	claimed, ok, err := svc.Store.ClaimQueuedOperation(ctx, 90*time.Second)
	if err != nil || !ok || claimed.ID != op.ID {
		t.Fatalf("fixture claim: %+v %v %v", claimed, ok, err)
	}
	svc.executeOperationJob(ctx, operationJob{operation: claimed}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := svc.Store.OperationByID(ctx, op.ID)
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("fixture deployment: %+v %v", result, err)
	}
	return result
}

func resetCrashGate(t *testing.T, ctx context.Context, db *store.Store, clusterID string) {
	t.Helper()
	if _, err := db.DB.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW() WHERE cluster_id=$1`, clusterID); err != nil {
		t.Fatal(err)
	}
}

func verifyIntegrationCrashMatrix(t *testing.T, ctx context.Context, svc *Service, kubeClient *kubernetes.Clientset, repo *integrationGitRepo, workspaceID, clusterID, sourceID, credentialID, ownerID, namespace string, key []byte) {
	db := svc.Store
	newApp := func(t *testing.T) store.Application {
		id := store.NewID()
		app := store.Application{ID: id, WorkspaceID: workspaceID, Name: "Crash " + id, SourceID: sourceID, Revision: "main", ManifestPath: "crash-matrix/" + id, Renderer: "yaml", ClusterID: clusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
		if err := db.CreateApplication(ctx, app); err != nil {
			t.Fatal(err)
		}
		return app
	}
	write := func(t *testing.T, app store.Application, value string) {
		for _, suffix := range []string{"a", "b"} {
			repo.write(t, app.ManifestPath+"/"+suffix+".yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: crash-%s-%s\n  namespace: %s\ndata:\n  mode: %s\n", app.ID[:8], suffix, namespace, value))
		}
		repo.commit(t, "crash fixture "+app.ID+" "+value)
	}
	for _, stage := range []string{"plan.before_save", "plan.saved"} {
		t.Run(stage, func(t *testing.T) {
			app := newApp(t)
			write(t, app, "desired")
			killWorkerAtCheckpoint(t, ctx, key, app.ID, ownerID, stage, "plan")
			plans, err := db.ListPlans(ctx, app.ID, 20)
			want := 0
			if stage == "plan.saved" {
				want = 1
			}
			if err != nil || len(plans) != want {
				t.Fatalf("planner crash left partial state: %d %v", len(plans), err)
			}
			restarted, err := svc.BuildPlan(ctx, app.ID, ownerID)
			if err != nil {
				t.Fatalf("planner restart: %v", err)
			}
			repeated, err := svc.BuildPlan(ctx, app.ID, ownerID)
			if err != nil || repeated.ID != restarted.ID || (want == 1 && restarted.ID != plans[0].ID) {
				t.Fatalf("planner retry did not reuse its durable result: %+v, %v", repeated, err)
			}
			var audits int
			if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE details->>'planId'=$1 AND action='plan.created'`, restarted.ID).Scan(&audits); err != nil || audits != 1 {
				t.Fatalf("planner crash lost or duplicated its audit event: %d, %v", audits, err)
			}
		})
	}
	for _, kind := range []string{"sync", "rollback", "decommission"} {
		mutationStage := "resource.applied"
		if kind == "decommission" {
			mutationStage = "resource.deleted"
		}
		for _, stage := range []string{"operation.claimed", mutationStage, "resource.recorded", "operation.before_complete", "operation.completed"} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				app := newApp(t)
				write(t, app, "old")
				var targetID string
				var plan store.PlanRecord
				var err error
				if kind != "sync" {
					initial, err := svc.BuildPlan(ctx, app.ID, ownerID)
					if err != nil {
						t.Fatal(err)
					}
					executeCrashFixture(t, ctx, svc, app, initial, ownerID)
					targets, err := svc.RollbackTargets(ctx, app.ID)
					if err != nil {
						t.Fatal(err)
					}
					for _, target := range targets {
						if target.Kind == "successful_sync" {
							targetID = target.ID
							break
						}
					}
				}
				if kind == "rollback" {
					write(t, app, "new")
					changed, err := svc.BuildPlan(ctx, app.ID, ownerID)
					if err != nil {
						t.Fatal(err)
					}
					executeCrashFixture(t, ctx, svc, app, changed, ownerID)
				}
				app, err = db.ApplicationByID(ctx, app.ID)
				if err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "sync":
					plan, err = svc.BuildPlan(ctx, app.ID, ownerID)
				case "rollback":
					plan, err = svc.BuildRollbackPlan(ctx, app, ownerID, "successful_sync", targetID, "")
				case "decommission":
					plan, err = svc.BuildDecommissionPlan(ctx, app, ownerID)
				}
				if err != nil {
					t.Fatal(err)
				}
				approvalIDs := []string{}
				if plan.Plan.RequiresApproval {
					approvalIDs = append(approvalIDs, createIntegrationApproval(t, ctx, db, plan, ownerID))
				}
				op, err := svc.ApplyWithApprovals(ctx, plan.ID, ownerID, approvalIDs)
				if err != nil {
					t.Fatal(err)
				}
				resetCrashGate(t, ctx, db, clusterID)
				killWorkerAtCheckpoint(t, ctx, key, app.ID, ownerID, stage, "operation")
				before, err := kubeClient.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: "justcd.io/application-id=" + app.ID})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.DB.ExecContext(ctx, `UPDATE operation_leases SET expires_at=NOW()-INTERVAL '1 second' WHERE operation_id=$1`, op.ID); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if err := db.RecoverInterruptedOperations(ctx); err != nil {
						t.Fatal(err)
					}
				}
				result, err := db.OperationByID(ctx, op.ID)
				if err != nil {
					t.Fatal(err)
				}
				expected := "failed"
				if stage == "operation.completed" {
					expected = "succeeded"
				}
				if result.Status != expected || (expected == "failed" && (result.TerminalReason != "interrupted_requires_review" || result.NextRetryAt != nil)) {
					t.Fatalf("unsafe crash recovery: %+v", result)
				}
				for _, resource := range before.Items {
					live, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, resource.Name, metav1.GetOptions{})
					if err != nil || live.UID != resource.UID || live.ResourceVersion != resource.ResourceVersion {
						t.Fatalf("recovery replayed a mutation: %s %v", resource.Name, err)
					}
				}
				if _, err := svc.ApplyWithApprovals(ctx, plan.ID, ownerID, approvalIDs); err == nil {
					t.Fatal("old plan/approval replay accepted")
				}
				if expected == "failed" {
					app, err = db.ApplicationByID(ctx, app.ID)
					if err != nil {
						t.Fatal(err)
					}
					if kind == "sync" {
						conflicts, err := svc.OwnershipConflicts(ctx, app)
						if err != nil {
							t.Fatal(err)
						}
						for _, conflict := range conflicts {
							if _, err := svc.AdoptResource(ctx, app, ownerID, "Review confirmed object from interrupted create", *conflict); err != nil {
								t.Fatal(err)
							}
						}
					}
					switch kind {
					case "sync":
						plan, err = svc.BuildPlan(ctx, app.ID, ownerID)
					case "rollback":
						plan, err = svc.BuildRollbackPlan(ctx, app, ownerID, "successful_sync", targetID, "")
					case "decommission":
						plan, err = svc.BuildDecommissionPlan(ctx, app, ownerID)
					}
					if err != nil {
						t.Fatalf("fresh reviewed recovery plan: %v", err)
					}
					ids := []string{}
					if plan.Plan.RequiresApproval {
						ids = append(ids, createIntegrationApproval(t, ctx, db, plan, ownerID))
					}
					fresh, err := svc.ApplyWithApprovals(ctx, plan.ID, ownerID, ids)
					if err != nil {
						t.Fatal(err)
					}
					resetCrashGate(t, ctx, db, clusterID)
					peer, err := store.Open(ctx, os.Getenv("JUSTCD_E2E_DATABASE_URL"))
					if err != nil {
						t.Fatal(err)
					}
					workerCtx, cancel := context.WithCancel(ctx)
					done := make(chan struct{}, 2)
					for _, replica := range []*Service{svc, {Store: peer, EncryptionKey: key}} {
						go func(s *Service) {
							s.RunOperationWorker(workerCtx, slog.New(slog.NewTextHandler(io.Discard, nil)))
							done <- struct{}{}
						}(replica)
					}
					func() {
						defer func() { cancel(); <-done; <-done; peer.DB.Close() }()
						waitIntegrationOperation(t, ctx, db, fresh.ID)
					}()
					if _, err := svc.ApplyWithApprovals(ctx, plan.ID, ownerID, ids); err == nil {
						t.Fatal("completed retry replay accepted")
					}
				}
				final, err := kubeClient.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: "justcd.io/application-id=" + app.ID})
				if err != nil {
					t.Fatal(err)
				}
				want := 2
				if kind == "decommission" {
					want = 0
				}
				if len(final.Items) != want {
					t.Fatalf("recovery final resource count: %d, want %d", len(final.Items), want)
				}
				for _, resource := range final.Items {
					if resource.Data["mode"] != "old" {
						t.Fatalf("recovery failed to converge: %+v", resource.Data)
					}
					for _, prior := range before.Items {
						if prior.Name == resource.Name && prior.UID != resource.UID {
							t.Fatal("recovery recreated an already-applied object")
						}
					}
				}
				var count int
				action := "sync.succeeded"
				if kind == "rollback" {
					action = "rollback.succeeded"
				}
				if expected == "failed" {
					action = op.Type + ".failed"
				}
				if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE details->>'operationId'=$1 AND action=$2`, op.ID, action).Scan(&count); err != nil || count != 1 {
					t.Fatalf("terminal crash event not exactly once: %d %v", count, err)
				}
			})
		}
	}
}
