package syncer

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func isNamespaceDeletion(change core.Change) bool {
	return change.Kind == core.Delete && change.Identity.APIVersion == "v1" && change.Identity.Kind == "Namespace" && change.Identity.ClusterScoped
}

// Retained namespaces remain visible as excluded changes in the reviewed plan.
func selectDecommissionNamespaces(plan *core.Plan, keep bool) {
	all := append(append([]core.Change{}, plan.Changes...), plan.Ignored...)
	plan.Changes = []core.Change{}
	plan.Ignored = nil
	plan.Selection = core.PlanSelection{KeepNamespaces: keep}
	sort.Slice(all, func(i, j int) bool { return all[i].Identity.Key() < all[j].Identity.Key() })
	for _, change := range all {
		if keep && isNamespaceDeletion(change) {
			plan.Ignored = append(plan.Ignored, change)
		} else {
			plan.Changes = append(plan.Changes, change)
		}
	}
}

// Derive a new immutable selection from a reviewed snapshot. No cluster writes
// occur here. Approval and apply still re-read live state before authorization.
func (s *Service) SelectDecommissionNamespaces(ctx context.Context, app store.Application, source store.PlanRecord, actorID string, keep bool) (store.PlanRecord, error) {
	if !source.Plan.Decommission || !app.Decommissioning || source.Status != "current" || !time.Now().Before(source.ExpiresAt) {
		return store.PlanRecord{}, errors.New("deletion plan is no longer current")
	}
	plan := source.Plan
	selectDecommissionNamespaces(&plan, keep)
	policy, err := s.Store.EffectiveApprovalPolicy(ctx, app)
	if err != nil {
		return store.PlanRecord{}, err
	}
	setApprovalRule(&plan, "deletion", policy.Deletion)
	setReviewReasons(&plan, app, false)
	if err := s.evaluateGuardrails(ctx, app, &plan, nil); err != nil {
		return store.PlanRecord{}, err
	}
	if err := core.RefreshDigest(&plan); err != nil {
		return store.PlanRecord{}, err
	}
	now := time.Now().UTC()
	record := store.PlanRecord{ID: store.NewID(), SourcePlanID: source.ID, Plan: plan, Desired: []core.Resource{}, CreatedBy: actorID, CreatedAt: now, ExpiresAt: source.ExpiresAt, Status: "current"}
	record, _, err = s.Store.SavePlanOrReuse(ctx, record)
	return record, err
}
