//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func testJustScanQueue(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	const endpoint = "https://scan.example.com"
	const workspace = "integration-workspace"
	const image = "registry.example.com/team/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	plan := PlanRecord{
		ID: NewID(), Plan: core.Plan{ApplicationID: "integration-app", Revision: "justscan"},
		CreatedBy: "integration-owner", ExpiresAt: time.Now().Add(time.Hour), Status: "current",
	}
	if err := s.SavePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err := s.BootstrapJustScan(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	connection := JustScanConnection{ServerURL: endpoint, OrganizationID: "test-org", TokenCipher: []byte("test-ciphertext")}
	if err := s.SaveJustScanConnection(ctx, workspace, connection); err != nil {
		t.Fatal(err)
	}
	queue := func() {
		t.Helper()
		if err := s.QueueJustScanPlanScans(ctx, workspace, plan.ID, []string{image}); err != nil {
			t.Fatal(err)
		}
	}
	queue()
	queue()
	if err := s.SetJustScanURL(ctx, "https://other.example.com", "integration-owner"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.ClaimJustScan(ctx); err != nil || ok {
		t.Fatalf("claim with an unrelated server: %t, %v", ok, err)
	}
	if err := s.BootstrapJustScan(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	if actual, err := s.JustScanURL(ctx); err != nil || actual != "https://other.example.com" {
		t.Fatalf("bootstrap overwrote admin configuration: %q, %v", actual, err)
	}
	if err := s.SetJustScanURL(ctx, endpoint, "integration-owner"); err != nil {
		t.Fatal(err)
	}
	work, ok, err := s.ClaimJustScan(ctx)
	if err != nil || !ok || work.Status != "submitting" || work.Image != image || work.OrganizationID != connection.OrganizationID {
		t.Fatalf("claim submission: %+v, %t, %v", work.JustScanPlanScan, ok, err)
	}
	if _, ok, err := s.ClaimJustScan(ctx); err != nil || ok {
		t.Fatalf("double claim: %t, %v", ok, err)
	}
	work.Status, work.ScanID = "pending", "remote-scan"
	if err := s.FinishJustScan(ctx, work); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE justscan_plan_scans SET next_at=NOW() WHERE plan_id=$1`, plan.ID); err != nil {
		t.Fatal(err)
	}
	next, ok, err := s.ClaimJustScan(ctx)
	if err != nil || !ok || next.Status != "pending" || next.ScanID != work.ScanID || next.AttemptID == work.AttemptID {
		t.Fatalf("claim poll: %+v, %t, %v", next.JustScanPlanScan, ok, err)
	}
	next.Status = "pass"
	if err := s.FinishJustScan(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJustScan(ctx, work); err != nil {
		t.Fatal(err)
	}
	queue()
	scans, err := s.JustScanPlanScans(ctx, plan.ID)
	if err != nil || len(scans) != 1 || scans[0].Status != "pass" {
		t.Fatalf("late or duplicate request modified result: %+v, %v", scans, err)
	}
	if err := s.DeleteJustScanConnection(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	scans, err = s.JustScanPlanScans(ctx, plan.ID)
	if err != nil || len(scans) != 0 {
		t.Fatalf("remove connection: %+v, %v", scans, err)
	}
}
