//go:build integration

package syncer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/schedule"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// Exercise the real executor's authorization path after a durable scheduled wait.
// This fixture has no Kubernetes credential, so it cannot write to a cluster.
func testScheduledApprovalExpiry(t *testing.T, ctx context.Context, db *store.Store, original store.Application, ownerID string) {
	t.Helper()
	for _, deferred := range []bool{false, true} {
		app := original
		app.ID = store.NewID()
		app.Name = "Schedule expiry " + store.NewID()
		app.SyncPolicy = "manual"
		if err := db.CreateApplication(ctx, app); err != nil {
			t.Fatal(err)
		}
		defer db.DB.ExecContext(ctx, `DELETE FROM applications WHERE id=$1`, app.ID)
		if deferred {
			if err := db.SetDeploymentPolicy(ctx, "application", app.ID, ownerID, schedule.Policy{Freezes: []schedule.Freeze{{Reason: "Waiting fixture"}}}); err != nil {
				t.Fatal(err)
			}
		}
		plan := core.Plan{ApplicationID: app.ID, Revision: "fixture", RequiredApprovals: 1, ApproverRoles: []string{"owner"}, Changes: []core.Change{}}
		if err := core.RefreshDigest(&plan); err != nil {
			t.Fatal(err)
		}
		record := store.PlanRecord{ID: store.NewID(), Plan: plan, CreatedBy: ownerID, ExpiresAt: time.Now().Add(time.Hour), Status: "current"}
		if err := db.SavePlan(ctx, record); err != nil {
			t.Fatal(err)
		}
		approvalID := store.NewID()
		approval := core.DeletionApproval{ActorID: ownerID, PlanDigest: plan.Digest, ExpiresAt: time.Now().Add(time.Hour)}
		if err := db.CreateApproval(ctx, approvalID, record.ID, approval, approval.ExpiresAt, "Scheduled review"); err != nil {
			t.Fatal(err)
		}
		queued, err := db.QueueOperation(ctx, app.ID, record.ID, ownerID, []string{approvalID}, plan.Digest, "", time.Minute, store.OperationProgress{})
		if err != nil {
			t.Fatal(err)
		}
		// Advance the fixture's history across expiry without sleeping for hours.
		for _, update := range []struct{ query, id string }{
			{`UPDATE operations SET status='running',started_at=NOW()-INTERVAL '2 hours' WHERE id=$1`, queued.ID},
			{`UPDATE plans SET expires_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, record.ID},
			{`UPDATE deletion_approvals SET expires_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, approvalID},
		} {
			if _, err := db.DB.ExecContext(ctx, update.query, update.id); err != nil {
				t.Fatal(err)
			}
		}
		persisted, err := db.OperationByID(ctx, queued.ID)
		if err != nil {
			t.Fatal(err)
		}
		result, err := (&Service{Store: db}).executeQueuedOperation(ctx, persisted)
		if err == nil || result.Status != "failed" {
			t.Fatalf("credential-free fixture must not apply: %+v %v", result, err)
		}
		expired := strings.Contains(err.Error(), "approval is expired")
		if expired == deferred {
			t.Fatalf("scheduled=%v got authorization error %v", deferred, err)
		}
	}
}
