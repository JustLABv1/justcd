//go:build integration

package store

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func testGuardrailPolicies(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	const workspaceID, appID, actor = "integration-workspace", "integration-app", "integration-owner"
	desired := []core.Resource{{Identity: core.Identity{APIVersion: "v1", Kind: "Service", Namespace: "default", Name: "public", ClusterID: "integration-cluster"}, Manifest: json.RawMessage(`{"spec":{"type":"NodePort"}}`)}}
	makePlan := func(mode string) PlanRecord {
		t.Helper()
		evaluation, err := (core.BuiltinPolicyEngine{}).Evaluate(core.PolicyConfig{Mode: mode}, desired)
		if err != nil {
			t.Fatal(err)
		}
		plan := core.Plan{ApplicationID: appID, Revision: "guardrails", Bindings: []core.Binding{}, Changes: []core.Change{}, PolicyEvaluation: &evaluation, RequiredApprovals: 1, RequiresApproval: true, ApprovalKind: "sync", ApproverRoles: []string{"owner"}}
		if err := core.RefreshDigest(&plan); err != nil {
			t.Fatal(err)
		}
		return PlanRecord{ID: NewID(), Plan: plan, Desired: desired, CreatedBy: actor, ExpiresAt: time.Now().Add(time.Hour), Status: "current"}
	}
	record := makePlan("audit")
	if _, _, err := s.SavePlanOrReuse(ctx, record); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.PlanByID(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Plan.PolicyEvaluation, record.Plan.PolicyEvaluation) {
		t.Fatal("policy results lost on load")
	}
	if err := core.RefreshDigest(&loaded.Plan); err != nil || loaded.Plan.Digest != record.Plan.Digest {
		t.Fatalf("JSONB roundtrip changed policy digest: %v", err)
	}
	plans, err := s.ListPlans(ctx, appID, 20)
	if err != nil || len(plans) == 0 || !reflect.DeepEqual(plans[0].Plan.PolicyEvaluation, record.Plan.PolicyEvaluation) {
		t.Fatalf("policy results lost on list: %v", err)
	}
	var details []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT details FROM audit_events WHERE resource_id=$1 AND action='plan.created' ORDER BY created_at DESC LIMIT 1`, appID).Scan(&details); err != nil || !strings.Contains(string(details), "public-services") {
		t.Fatalf("audit lost policy results: %s %v", details, err)
	}
	approvalID := NewID()
	approval := core.DeletionApproval{ActorID: actor, PlanDigest: record.Plan.Digest, ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateApproval(ctx, approvalID, record.ID, approval, approval.ExpiresAt, ""); err != nil {
		t.Fatal(err)
	}
	op, err := s.QueueOperation(ctx, appID, record.ID, actor, []string{approvalID}, record.Plan.Digest, "", time.Minute, OperationProgress{})
	if err != nil {
		t.Fatalf("audit must allow enqueue: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE operations SET status='running' WHERE id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginOperationResource(ctx, op.ID, OperationProgress{}); err != nil {
		t.Fatalf("audit must allow resource step: %v", err)
	}
	if err := s.SetGuardrailPolicy(ctx, workspaceID, actor, core.PolicyConfig{Mode: "block"}); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginOperationResource(ctx, op.ID, OperationProgress{}); err == nil {
		t.Fatal("changed policy allowed approved running plan to mutate")
	}
	stale, err := s.PlanByID(ctx, record.ID)
	if err != nil || stale.Status != "stale" {
		t.Fatalf("config did not invalidate plan: %+v %v", stale, err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM operation_leases WHERE operation_id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE operations SET status='failed',finished_at=NOW() WHERE id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueOperation(ctx, appID, record.ID, actor, []string{approvalID}, record.Plan.Digest, "", time.Minute, OperationProgress{}); err == nil {
		t.Fatal("changed policy allowed previously approved plan")
	}
	record = makePlan("block")
	if err := s.SavePlan(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPlanGuardrails(ctx, record.ID); err == nil {
		t.Fatal("blocking results ignored")
	}
	if err := s.CreateApproval(ctx, NewID(), record.ID, core.DeletionApproval{ActorID: actor, PlanDigest: record.Plan.Digest, ExpiresAt: time.Now().Add(time.Hour)}, time.Now().Add(time.Hour), ""); err == nil {
		t.Fatal("blocking plan approved")
	}
	if _, err := s.QueueOperation(ctx, appID, record.ID, actor, nil, record.Plan.Digest, "", time.Minute, OperationProgress{}); err == nil {
		t.Fatal("blocking plan enqueued")
	}
	if err := s.SetGuardrailPolicy(ctx, workspaceID, actor, core.PolicyConfig{Mode: "block"}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := s.PlanByID(ctx, record.ID)
	if err != nil || unchanged.Status != "current" {
		t.Fatal("identical config invalidated plan")
	}
	if err := s.SetGuardrailPolicy(ctx, workspaceID, actor, core.PolicyConfig{Mode: "audit"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePlan(ctx, makePlan("block")); err == nil {
		t.Fatal("stale config committed after invalidation")
	}
}
