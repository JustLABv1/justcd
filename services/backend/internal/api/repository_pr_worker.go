package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) processRepositoryPRReview(ctx context.Context, c store.SourceControlConnection, review store.PullRequestReview) error {
	v, err := s.Store.RepositoryPRByID(ctx, *c.RepositoryPRID)
	if err != nil {
		return err
	}
	if review.Number != v.Number {
		ignored := review
		ignored.Phase = "ignored"
		ignored.PreviewApplicationID = nil
		_, err := s.Store.UpdateReviewResult(ctx, ignored, review.HeadSHA, review.Closed)
		return err
	}
	if v.Phase == "promoted" || v.ApplicationID == nil {
		return nil
	}
	repo, err := s.Store.RepositoryConfigurationByID(ctx, v.RepositoryID)
	if err != nil {
		return err
	}
	token, err := s.sourceControlToken(ctx, c)
	if err != nil {
		_ = s.Store.SetReviewWorkerError(ctx, review.ID, err.Error())
		review.Phase = "failed"
		review.Error = err.Error()
		_ = s.Store.UpdateRepositoryPRResult(ctx, v.ID, review)
		return err
	}
	current, err := (scm.Client{}).Current(ctx, c.Provider, c.APIURL, c.Repository, string(token), review.Number)
	if err != nil {
		_ = s.Store.SetReviewWorkerError(ctx, review.ID, "could not read current PR state: "+err.Error())
		review.Phase = "failed"
		review.Error = err.Error()
		_ = s.Store.UpdateRepositoryPRResult(ctx, v.ID, review)
		return err
	}
	if current.HeadSHA != review.HeadSHA || current.Closed != review.Closed || current.HeadBranch != review.HeadBranch || current.Fork != review.Fork {
		if err = s.refreshRepositoryPREvent(ctx, c.ID, current); err != nil {
			return err
		}
		return nil
	}
	app, err := s.Store.ApplicationByID(ctx, *v.ApplicationID)
	if err != nil {
		return err
	}
	result := review
	result.PreviewApplicationID = &app.ID
	result.SharedEnvironment = v.Mode == "existing"
	result.ProcessedSHA = review.HeadSHA
	result.Error = ""
	expired := result.ExpiresAt != nil && !result.ExpiresAt.After(time.Now())
	if current.Closed && current.Merged && !v.DefinitionRemoved && current.TargetBranch == strings.TrimPrefix(repo.Revision, "refs/heads/") && v.Mode == "existing" {
		_, err = s.Store.DB.ExecContext(ctx, `UPDATE repository_pr_applications SET merged=TRUE,phase='awaiting_merge',updated_at=NOW() WHERE id=$1`, v.ID)
		if err != nil {
			return err
		}
		if err = s.Syncer.ReconcileRepository(ctx, repo.ID); err != nil {
			result.Phase = "cleanup_pending"
			result.Error = "Waiting for tracked-branch discovery to take over: " + err.Error()
		} else {
			fresh, e := s.Store.RepositoryPRByID(ctx, v.ID)
			if e != nil {
				return e
			}
			if fresh.Phase == "promoted" {
				return nil
			}
			result.Phase = "cleanup_pending"
			result.Error = "Waiting for the merged definition on the tracked branch"
		}
	} else if current.Closed || expired || v.DefinitionRemoved {
		removed, e := s.cleanupRepositoryPRApplication(ctx, c, v, app, &result)
		if e != nil {
			return e
		}
		if removed {
			return nil
		}
	} else if !repo.Enabled || !repo.PRSettings.Enabled || !c.Enabled {
		result.Phase = "blocked"
		result.Error = "repository PR discovery is paused"
	} else if current.Fork || current.TargetBranch != strings.TrimPrefix(repo.Revision, "refs/heads/") {
		result.Phase = "blocked"
		result.Error = "PR is a fork or targets a different tracked branch"
	} else if app.Revision != review.HeadSHA || v.HeadSHA != review.HeadSHA {
		result.Phase = "pending"
		result.Error = "Waiting for repository discovery to validate the new commit"
	} else if v.Mode == "review-only" {
		if review.Phase != "planned" || review.ProcessedSHA != review.HeadSHA {
			plan, _, e := s.Syncer.CalculatePlanWithSelection(ctx, app, core.PlanSelection{})
			if e != nil {
				result.Phase = "failed"
				result.Error = e.Error()
			} else if plan.Revision != review.HeadSHA {
				result.Phase = "pending"
				result.Error = "PR head changed while planning"
			} else {
				result.Plan, _ = json.Marshal(toPlanView(store.PlanRecord{ID: review.ID, Plan: plan, Status: "review-only", CreatedAt: time.Now().UTC()}))
				result.Phase = "planned"
			}
		}
	} else if review.ProcessedSHA == review.HeadSHA && !s.reviewPlanNeedsRefresh(ctx, review) && (review.Phase == "approval_required" || review.Phase == "syncing" || review.Phase == "ready" || review.Phase == "degraded") {
		s.refreshPreviewReview(ctx, &result)
	} else {
		s.buildPreviewReview(ctx, app, c, &result)
	}
	if err = s.applyPRCommentApprovals(ctx, c, &result, string(token)); err != nil {
		result.Error = "PR comment approval check failed: " + err.Error()
	}
	changed, err := s.Store.UpdateReviewResult(ctx, result, review.HeadSHA, review.Closed)
	if err != nil || !changed {
		return err
	}
	if err = s.Store.UpdateRepositoryPRResult(ctx, v.ID, result); err != nil {
		return err
	}
	return s.reportReview(ctx, c, result, string(token))
}

func (s *Server) cleanupRepositoryPRApplication(ctx context.Context, c store.SourceControlConnection, v store.RepositoryPRApplication, app store.Application, result *store.PullRequestReview) (bool, error) {
	result.Phase = "cleanup_pending"
	active, err := s.Store.ApplicationHasActiveOperation(ctx, app.ID)
	if err != nil {
		return false, err
	}
	if active {
		result.Error = "Waiting for the current deployment to finish"
		return false, nil
	}
	managed, err := s.Store.ManagedResources(ctx, app.ID)
	if err != nil {
		return false, err
	}
	if len(managed) == 0 && (app.Health == "decommissioned" || app.LastSyncedRevision == "" && !app.Decommissioning) {
		if v.Mode == "isolated" {
			if err = s.removePreviewNamespace(ctx, c, *result, app.Namespaces[0].Namespace); err != nil {
				result.Error = "Namespace cleanup is waiting: " + err.Error()
				return false, nil
			}
		}
		result.Phase = "removed"
		result.PreviewApplicationID = nil
		_ = s.Store.UpdateRepositoryPRResult(ctx, v.ID, *result)
		// Retain the independent repository record; deleting the app cascades its connection/review.
		if _, err = s.Store.DeleteApplicationKeepingResources(ctx, app.ID, true); err != nil {
			return false, err
		}
		if v.Mode == "isolated" {
			_ = s.Store.DeletePreviewNamespaceBinding(ctx, app.WorkspaceID, app.ClusterID, app.Namespaces[0].Namespace)
		}
		return true, nil
	}
	var view planView
	if json.Unmarshal(result.Plan, &view) == nil {
		if p, e := s.Store.PlanByID(ctx, view.ID); e == nil && p.Plan.Decommission && p.Status == "current" && p.ExpiresAt.After(time.Now()) {
			return false, nil
		}
	}
	plan, err := s.Syncer.BuildDecommissionPlan(ctx, app, reviewActorID)
	if err != nil {
		result.Error = "Could not prepare reviewed cleanup: " + err.Error()
		return false, nil
	}
	result.Plan, _ = json.Marshal(toPlanView(plan))
	return false, nil
}

func (s *Server) validateRepositoryPRDeployment(ctx context.Context, c store.SourceControlConnection, review store.PullRequestReview, app store.Application) error {
	v, err := s.Store.RepositoryPRByID(ctx, *c.RepositoryPRID)
	if err != nil {
		return err
	}
	repo, err := s.Store.RepositoryConfigurationByID(ctx, v.RepositoryID)
	if err != nil {
		return err
	}

	if !repo.Enabled || !repo.PRSettings.Enabled || v.Mode == "review-only" || v.Number != review.Number || v.DefinitionRemoved || v.ApplicationID == nil || *v.ApplicationID != app.ID || v.HeadSHA != review.HeadSHA || app.Revision != review.HeadSHA || review.Fork {
		return errors.New("repository PR policy does not permit this deployment")
	}
	trusted, err := s.repositoryPRConnection(ctx, repo)
	if err != nil || trusted.Provider != c.Provider || trusted.APIURL != c.APIURL || trusted.Repository != c.Repository || app.SourceID != repo.SourceID {
		return errors.New("repository Git source or PR provider changed; review its configuration")
	}
	if v.Mode == "existing" {
		_, desired, err := s.Syncer.CalculatePlanWithSelection(ctx, app, core.PlanSelection{})
		if err != nil {
			return err
		}
		if len(app.Namespaces) != 1 {
			return errors.New("PR namespace binding changed")
		}
		if err = validateRepositoryPRResourceScope(desired, app.Namespaces[0].Namespace); err != nil {
			return err
		}
	}
	if v.Mode == "existing" && (len(app.Namespaces) != 1 || !repo.PRSettings.Allows(app.ClusterID, app.Namespaces[0].Namespace)) {
		return errors.New("repository PR destination is no longer allowed")
	}
	return nil
}

func validateRepositoryPRResourceScope(resources []core.Resource, namespace string) error {
	for _, resource := range resources {
		if resource.Identity.ClusterScoped || resource.Identity.Namespace != namespace {
			return errors.New("repository PR applications may only manage resources in their approved namespace")
		}
	}
	return nil
}
