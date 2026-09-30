//go:build integration

package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func testSyncPauseSafety(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	const appID = "integration-app"
	if err := s.PauseApplication(ctx, appID); err != nil {
		t.Fatal(err)
	}
	app, err := s.ApplicationByID(ctx, appID)
	if err != nil || !app.AutoSyncPaused || app.SyncPolicy != "manual" {
		t.Fatalf("pause must preserve policy: %+v, %v", app, err)
	}
	if _, err := s.QueueOperation(ctx, appID, "missing-plan", "justcd-system", nil, "", "", time.Minute, OperationProgress{}); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("stale automatic enqueue must reject pause: %v", err)
	}
	id, err := s.AcquireResourceAction(ctx, appID, "integration-owner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PauseApplication(ctx, appID); err == nil {
		t.Fatal("pause must reject an active operation")
	}
	if _, err := s.AcquireResourceAction(ctx, appID, "integration-owner", time.Minute); err == nil {
		t.Fatal("resource actions must exclude concurrent actions")
	}
	if err := s.ResumeRollbackTracking(ctx, appID, ""); err == nil {
		t.Fatal("resume must reject an active resource action")
	}
	op, err := s.OperationByID(ctx, id)
	if err != nil || op.Type != "resource_action" {
		t.Fatalf("resource action operation type: %+v, %v", op, err)
	}
	if err := s.FinishOperation(ctx, id, "succeeded", "test action"); err != nil {
		t.Fatal(err)
	}
	app, err = s.ApplicationByID(ctx, appID)
	if err != nil || !app.AutoSyncPaused {
		t.Fatalf("action must stay paused: %+v, %v", app, err)
	}
	if err := s.ResumeRollbackTracking(ctx, appID, ""); err != nil {
		t.Fatal(err)
	}
	app, err = s.ApplicationByID(ctx, appID)
	if err != nil || app.AutoSyncPaused || app.SyncPolicy != "manual" {
		t.Fatalf("resume must preserve policy: %+v, %v", app, err)
	}
}
