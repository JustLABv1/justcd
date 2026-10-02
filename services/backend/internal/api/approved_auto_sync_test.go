package api

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestApprovedAutoSyncEligibility(t *testing.T) {
	for _, tc := range []struct {
		name       string
		changeApp  func(*store.Application)
		changePlan func(*core.Plan)
		approvals  int
		want       bool
	}{
		{name: "final approval", approvals: 2, want: true},
		{name: "partial approval", approvals: 1},
		{name: "manual", changeApp: func(a *store.Application) { a.SyncPolicy = "manual" }, approvals: 2},
		{name: "paused", changeApp: func(a *store.Application) { a.AutoSyncPaused = true }, approvals: 2},
		{name: "decommissioning", changeApp: func(a *store.Application) { a.Decommissioning = true }, approvals: 2},
		{name: "definition missing", changeApp: func(a *store.Application) { a.ConfigurationMissing = true }, approvals: 2},
		{name: "rollback", changePlan: func(p *core.Plan) { p.Rollback = &core.RollbackTarget{} }, approvals: 2},
		{name: "application deletion", changePlan: func(p *core.Plan) { p.Decommission = true }, approvals: 2},
		{name: "no approval required", changePlan: func(p *core.Plan) { p.RequiredApprovals = 0; p.RequiresApproval = false }, approvals: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := store.Application{SyncPolicy: "auto-safe"}
			plan := core.Plan{RequiresApproval: true, RequiredApprovals: 2}
			if tc.changeApp != nil {
				tc.changeApp(&app)
			}
			if tc.changePlan != nil {
				tc.changePlan(&plan)
			}
			if got := shouldAutoApplyApprovedPlan(app, plan, tc.approvals); got != tc.want {
				t.Fatalf("automatic queue=%v, want %v", got, tc.want)
			}
		})
	}
}
