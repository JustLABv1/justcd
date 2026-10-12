//go:build integration

package store

import (
	"context"
	"github.com/justlab/justcd/services/backend/internal/core"
	"reflect"
	"testing"
	"time"
)

func testPlanReviewPersistence(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	for _, modern := range []bool{false, true} {
		plan := core.Plan{ApplicationID: "integration-app", Revision: "review", Bindings: []core.Binding{}, Changes: []core.Change{}, RequiredApprovals: 1, RequiresApproval: true, ApprovalKind: "rollback", ApproverRoles: []string{"owner"}}
		if modern {
			plan.ApprovalReasons = []core.ApprovalReason{{Code: "rollback", Rule: "safeguard.rollback", Description: "Owner review", Resources: []core.Identity{}}}
		}
		if err := core.RefreshDigest(&plan); err != nil {
			t.Fatal(err)
		}
		record := PlanRecord{ID: NewID(), Plan: plan, CreatedBy: "integration-owner", ExpiresAt: time.Now().Add(time.Hour), Status: "current"}
		if err := s.SavePlan(ctx, record); err != nil {
			t.Fatal(err)
		}
		loaded, err := s.PlanByID(ctx, record.ID)
		if err != nil {
			t.Fatal(err)
		}
		list, err := s.ListPlans(ctx, plan.ApplicationID, 20)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range list {
			if item.ID == record.ID {
				found = true
				if !reflect.DeepEqual(item.Plan.ApprovalReasons, plan.ApprovalReasons) {
					t.Fatalf("list lost reasons: %+v", item.Plan)
				}
			}
		}
		if !found {
			t.Fatal("plan not listed")
		}
		if !reflect.DeepEqual(loaded.Plan.ApprovalReasons, plan.ApprovalReasons) {
			t.Fatalf("load lost reasons: %+v", loaded.Plan)
		}
		if err := core.RefreshDigest(&loaded.Plan); err != nil {
			t.Fatal(err)
		}
		if loaded.Plan.Digest != record.Plan.Digest {
			t.Fatal("JSONB plan roundtrip changed digest")
		}
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id='integration-app'`); err != nil {
		t.Fatal(err)
	}
}
