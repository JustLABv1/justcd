package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

const reviewActorID = "justcd-system"

func (s *Server) RunReviewWorker(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items, err := s.Store.DueReviews(ctx, 10)
			if err != nil {
				logger.Error("could not list due pull request reviews", "error", err)
				continue
			}
			for _, item := range items {
				if ctx.Err() != nil {
					return
				}
				workCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				if err := s.processReview(workCtx, item); err != nil {
					logger.Warn("pull request review failed", "reviewId", item.ID, "error", err)
				}
				cancel()
				_ = s.Store.ReleaseReviewLease(ctx, item.ID)
			}
		}
	}
}

func (s *Server) processReview(ctx context.Context, review store.PullRequestReview) error {
	connection, err := s.Store.SourceControlConnectionByID(ctx, review.ConnectionID)
	if err != nil {
		return err
	}
	token, err := security.Decrypt(s.EncryptionKey, connection.StatusTokenCipher, "source-control-status:"+connection.ID)
	if err != nil {
		return err
	}
	expired := review.ExpiresAt != nil && !review.ExpiresAt.After(time.Now())
	if !expired {
		client := scm.Client{}
		current, err := client.Current(ctx, connection.Provider, connection.APIURL, connection.Repository, string(token), review.Number)
		if err != nil {
			_ = s.Store.SetReviewWorkerError(ctx, review.ID, "could not read current pull request state: "+err.Error())
			return err
		}
		if current.HeadSHA != review.HeadSHA || current.Closed != review.Closed || current.Fork != review.Fork {
			// The provider is authoritative; webhook deliveries may be delayed or reordered.
			fresh := store.PullRequestReview{ID: store.NewID(), ConnectionID: review.ConnectionID, Number: review.Number, HeadSHA: current.HeadSHA, SourceURL: current.URL, Fork: current.Fork, Closed: current.Closed, EventAt: time.Now().UTC()}
			_, err = s.Store.RecordReviewEvent(ctx, review.ConnectionID, "provider-refresh:"+review.ID+":"+current.HeadSHA+":"+strconv.FormatBool(current.Closed), fresh)
			return err
		}
	}
	if review.Phase == "removed" {
		if review.ReportedPhase != review.Phase {
			return s.reportReview(ctx, connection, review, string(token))
		}
		return nil
	}
	if review.Phase == "planned" && review.ProcessedSHA == review.HeadSHA && (review.ExpiresAt == nil || review.ExpiresAt.After(time.Now())) {
		if review.ReportedPhase != review.Phase {
			return s.reportReview(ctx, connection, review, string(token))
		}
		return nil
	}
	app, err := s.Store.ApplicationByID(ctx, connection.ApplicationID)
	if err != nil {
		return err
	}
	source, sourceErr := s.Store.GitSourceForWorkspace(ctx, app.SourceID, app.WorkspaceID)
	apiURL, parseErr := url.Parse(connection.APIURL)
	if sourceErr != nil || parseErr != nil || !sourceMatchesConnection(source.RepositoryURL, connection.Provider, apiURL, connection.Repository) {
		_ = s.Store.SetReviewWorkerError(ctx, review.ID, "application Git source no longer matches source control connection")
		return errors.New("application Git source no longer matches source control connection")
	}
	result := review
	result.Error = ""
	result.Plan = nil
	result.ProcessedSHA = review.HeadSHA
	if review.Closed || (review.ExpiresAt != nil && !review.ExpiresAt.After(time.Now())) {
		s.handleClosedReview(ctx, connection, &result)
	} else if review.Fork && !connection.PreviewProfile.AllowForks {
		result.Phase = "blocked"
		result.Error = "fork pull requests require an explicit owner opt-in"
	} else if connection.PreviewProfile.Enabled {
		if review.ProcessedSHA == review.HeadSHA && (review.Phase == "syncing" || review.Phase == "approval_required" || review.Phase == "ready" || review.Phase == "degraded") {
			s.refreshPreviewReview(ctx, &result)
		} else {
			s.buildPreviewReview(ctx, app, connection, &result)
		}
	} else {
		candidate := app
		candidate.Revision = pullRequestRef(connection.Provider, review.Number)
		// CalculatePlan never saves an applyable plan. The result is stored only in
		// pull_request_reviews and cannot be passed to the normal apply endpoint.
		plan, _, planErr := s.Syncer.CalculatePlanWithSelection(ctx, candidate, core.PlanSelection{})
		if planErr == nil && plan.Revision != review.HeadSHA {
			planErr = errors.New("pull request head changed while planning")
		}
		if planErr != nil {
			result.Phase = "failed"
			result.Error = "could not calculate PR plan: " + planErr.Error()
		} else {
			view := toPlanView(store.PlanRecord{ID: review.ID, Plan: plan, Status: "review-only", CreatedAt: time.Now().UTC()})
			result.Plan, _ = json.Marshal(view)
			result.Phase = "planned"
		}
	}
	changed, err := s.Store.UpdateReviewResult(ctx, result, review.HeadSHA, review.Closed)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	_ = s.Store.Audit(ctx, reviewActorID, "pull_request.reviewed", "application", app.ID, map[string]any{"reviewId": review.ID, "number": review.Number, "revision": review.HeadSHA, "phase": result.Phase})
	return s.reportReview(ctx, connection, result, string(token))
}

func (s *Server) buildPreviewReview(ctx context.Context, production store.Application, connection store.SourceControlConnection, result *store.PullRequestReview) {
	preview, namespace, err := s.previewApplication(ctx, production, connection, *result)
	if err != nil {
		result.Phase = "failed"
		result.Error = err.Error()
		return
	}
	result.PreviewApplicationID = &preview.ID
	if result.ExpiresAt == nil {
		expiry := time.Now().Add(time.Duration(connection.PreviewProfile.MaxLifetimeHours) * time.Hour)
		result.ExpiresAt = &expiry
	}
	// The first calculation is a validation gate. An untrusted preview cannot
	// create secrets, use production hosts, or create cluster-scoped resources.
	calculated, desired, err := s.Syncer.CalculatePlanWithSelection(ctx, preview, core.PlanSelection{})
	if err == nil && calculated.Revision != result.HeadSHA {
		err = errors.New("pull request head changed while planning")
	}
	if err != nil {
		result.Phase = "failed"
		result.Error = "preview plan failed: " + err.Error()
		return
	}
	if err = validatePreviewResources(desired, connection.PreviewProfile, namespace, result.Fork); err != nil {
		result.Phase = "failed"
		result.Error = err.Error()
		return
	}
	if err = s.ensurePreviewNamespace(ctx, connection, *result, namespace); err != nil {
		result.Phase = "failed"
		result.Error = "preview namespace setup failed: " + err.Error()
		return
	}
	plan, err := s.Syncer.BuildPlan(ctx, preview.ID, reviewActorID)
	if err != nil {
		result.Phase = "failed"
		result.Error = "preview plan failed: " + err.Error()
		return
	}
	if plan.Plan.Revision != result.HeadSHA {
		result.Phase = "failed"
		result.Error = "pull request head changed while planning"
		return
	}
	result.Plan, _ = json.Marshal(toPlanView(plan))
	if plan.Plan.RequiresApproval || result.Fork || planContainsDeletion(plan.Plan) {
		result.Phase = "approval_required"
		return
	}
	if len(plan.Plan.Changes) == 0 {
		_ = s.Store.SetPlanStatus(ctx, plan.ID, "applied")
		_ = s.Store.MarkApplicationSynced(ctx, preview.ID, plan.Plan.Revision, "synced")
		result.Phase = "syncing"
		return
	}
	if _, err = s.Syncer.Apply(ctx, plan.ID, reviewActorID, ""); err != nil {
		result.Phase = "failed"
		result.Error = "could not queue preview sync: " + err.Error()
		return
	}
	result.Phase = "syncing"
}

func planContainsDeletion(plan core.Plan) bool {
	for _, change := range plan.Changes {
		if change.Kind == core.Delete {
			return true
		}
	}
	return false
}

func (s *Server) refreshPreviewReview(ctx context.Context, result *store.PullRequestReview) {
	if result.PreviewApplicationID == nil {
		result.Phase = "failed"
		result.Error = "preview application is missing"
		return
	}
	app, err := s.Store.ApplicationByID(ctx, *result.PreviewApplicationID)
	if err != nil {
		result.Phase = "failed"
		result.Error = "preview application is missing"
		return
	}
	if app.LastSyncedRevision != result.HeadSHA {
		return
	}
	if err := s.Syncer.RefreshApplicationHealth(ctx, app); err != nil {
		return
	}
	app, err = s.Store.ApplicationByID(ctx, app.ID)
	if err != nil {
		return
	}
	switch app.HealthCondition.Status {
	case apphealth.Healthy:
		result.Phase = "ready"
	case apphealth.Degraded, apphealth.Missing:
		result.Phase = "degraded"
	default:
		result.Phase = "syncing"
	}
}

func (s *Server) handleClosedReview(ctx context.Context, connection store.SourceControlConnection, result *store.PullRequestReview) {
	result.Phase = "closed"
	if result.PreviewApplicationID == nil {
		id, _ := previewIdentity(connection.ID, result.Number, connection.PreviewProfile.NamespacePrefix)
		if _, err := s.Store.ApplicationByID(ctx, id); err == nil {
			result.PreviewApplicationID = &id
		} else {
			if errors.Is(err, sql.ErrNoRows) {
				_ = s.Store.ReleasePreviewSlot(ctx, connection.ID, result.Number)
			}
			return
		}
	}
	preview, err := s.Store.ApplicationByID(ctx, *result.PreviewApplicationID)
	if errors.Is(err, sql.ErrNoRows) {
		result.Phase = "failed"
		result.Error = "preview application was removed outside cleanup flow"
		return
	}
	if err != nil {
		result.Phase = "failed"
		result.Error = "could not load preview for cleanup"
		return
	}
	managed, managedErr := s.Store.ManagedResources(ctx, preview.ID)
	active, activeErr := s.Store.ApplicationHasActiveOperation(ctx, preview.ID)
	if managedErr != nil || activeErr != nil {
		result.Phase = "cleanup_pending"
		result.Error = "could not verify preview cleanup state"
		return
	}
	if active {
		result.Phase = "cleanup_pending"
		result.Error = "preview operation is still running"
		return
	}
	if len(managed) == 0 && (preview.Health == "decommissioned" || (preview.LastSyncedRevision == "" && !preview.Decommissioning)) {
		if len(preview.Namespaces) != 1 {
			result.Phase = "failed"
			result.Error = "preview namespace binding is missing"
			return
		}
		namespace := preview.Namespaces[0].Namespace
		if err := s.removePreviewNamespace(ctx, connection, *result, namespace); err != nil {
			result.Phase = "cleanup_pending"
			result.Error = "namespace deletion is waiting or needs review"
			return
		}
		if _, err := s.Store.DeleteApplicationKeepingResources(ctx, preview.ID, true); err != nil {
			result.Phase = "cleanup_pending"
			result.Error = "application cleanup is waiting"
			return
		}
		_ = s.Store.DeletePreviewNamespaceBinding(ctx, preview.WorkspaceID, preview.ClusterID, namespace)
		_ = s.Store.ReleasePreviewSlot(ctx, connection.ID, result.Number)
		result.PreviewApplicationID = nil
		result.Phase = "removed"
		return
	}
	plan, err := s.Syncer.BuildDecommissionPlan(ctx, preview, reviewActorID)
	if err != nil {
		result.Phase = "failed"
		result.Error = "could not prepare preview cleanup"
		return
	}
	result.Phase = "cleanup_pending"
	result.Plan, _ = json.Marshal(toPlanView(plan))
}

func (s *Server) reportReview(ctx context.Context, connection store.SourceControlConnection, review store.PullRequestReview, token string) error {
	state, description := "pending", "JustCD is preparing a PR plan"
	switch review.Phase {
	case "planned":
		state, description = "success", "JustCD PR plan is ready for review"
	case "ready":
		state, description = "success", "JustCD preview is healthy"
	case "approval_required":
		state, description = "pending", "JustCD preview plan needs approval"
	case "syncing":
		state, description = "running", "JustCD preview is deploying"
	case "cleanup_pending":
		state, description = "pending", "JustCD preview cleanup needs review"
	case "closed":
		state, description = "success", "JustCD preview is closed"
	case "removed":
		state, description = "success", "JustCD preview has been removed"
	case "blocked":
		state, description = "failed", "JustCD blocked this pull request"
	case "failed", "degraded":
		state, description = "failed", "JustCD could not prepare a healthy preview"
	}
	target := s.Config.FrontendURL + "/applications/" + connection.ApplicationID + "/pull-requests#review-" + strconv.Itoa(review.Number)
	if err := (scm.Client{}).Report(ctx, connection.Provider, connection.APIURL, connection.Repository, token, review.HeadSHA, state, description, target, review.Number); err != nil {
		_ = s.Store.SetReviewReportError(ctx, review.ID, "could not update provider commit status: "+err.Error())
		return err
	}
	return s.Store.MarkReviewReported(ctx, review.ID, review.Phase)
}
