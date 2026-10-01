package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func approvalCommand(body, planID, digest string) bool {
	parts := strings.Fields(strings.TrimSpace(body))
	return len(parts) == 4 && parts[0] == "/justcd" && parts[1] == "approve" && parts[2] == planID && parts[3] == digest
}

func (s *Server) applyPRCommentApprovals(ctx context.Context, connection store.SourceControlConnection, review *store.PullRequestReview, token string) error {
	if review.Phase != "approval_required" && review.Phase != "cleanup_pending" || len(review.Plan) == 0 {
		return nil
	}
	var view planView
	if err := json.Unmarshal(review.Plan, &view); err != nil {
		return err
	}
	record, err := s.Store.PlanByID(ctx, view.ID)
	if err != nil || record.Status != "current" || !record.ExpiresAt.After(time.Now()) {
		return nil
	}
	if !record.Plan.Decommission && record.Plan.Revision != review.HeadSHA {
		return nil
	}
	comments, err := (scm.Client{}).Comments(ctx, connection.Provider, connection.APIURL, connection.Repository, token, review.Number)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if comment.System || comment.CreatedAt.Before(record.CreatedAt) || !comment.CreatedAt.Before(record.ExpiresAt) || !approvalCommand(comment.Body, record.ID, record.Plan.Digest) {
			continue
		}
		providerID := comment.User.ID
		if connection.Provider == "gitlab" {
			providerID = comment.Author.ID
		}
		actorID := connection.PreviewProfile.ApprovalActors[strconv.FormatInt(providerID, 10)]
		if actorID == "" {
			continue
		}
		actor, err := s.Store.UserByID(ctx, actorID)
		if err != nil {
			continue
		}
		app, err := s.Store.ApplicationByID(ctx, record.Plan.ApplicationID)
		if err != nil {
			return err
		}
		role, err := s.Store.WorkspaceRole(ctx, actor, app.WorkspaceID)
		if err != nil || !core.ApprovalRoleAllows(record.Plan, role, actorID) {
			continue
		}
		deletes, privileged := []core.Change{}, []core.Change{}
		for _, change := range record.Plan.Changes {
			if change.Kind == core.Delete {
				deletes = append(deletes, change)
			}
			if change.Identity.ClusterScoped {
				privileged = append(privileged, change)
			}
		}
		approval := core.DeletionApproval{ActorID: actorID, PlanDigest: record.Plan.Digest, ExpiresAt: record.ExpiresAt, Deletes: deletes, Privileged: privileged}
		if err = s.Store.CreateApproval(ctx, store.NewID(), record.ID, approval, record.ExpiresAt, "PR comment "+strconv.FormatInt(comment.ID, 10)); err != nil {
			if strings.Contains(err.Error(), "already") || strings.Contains(err.Error(), "required approvals") {
				continue
			}
			return err
		}
		_ = s.Store.Audit(ctx, actorID, "pull_request.plan_approved", "application", app.ID, map[string]any{"reviewId": review.ID, "planId": record.ID, "digest": record.Plan.Digest, "providerUserId": providerID, "commentId": comment.ID})
	}
	approvals, err := s.Store.ListPlanApprovals(ctx, record.ID, record.Plan.Digest)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, approval := range approvals {
		ids = append(ids, approval.ID)
	}
	if len(ids) < core.RequiredApprovalCount(record.Plan) {
		return nil
	}
	// Verify the provider again immediately before queueing, even when a webhook
	// or polling update has not reached our review row yet.
	current, err := (scm.Client{}).Current(ctx, connection.Provider, connection.APIURL, connection.Repository, token, review.Number)
	if err != nil {
		return err
	}
	if current.HeadSHA != review.HeadSHA || current.Closed != review.Closed || current.Fork != review.Fork {
		return fmt.Errorf("PR changed before applying approval")
	}
	if !record.Plan.Decommission {
		app, err := s.Store.ApplicationByID(ctx, record.Plan.ApplicationID)
		if err != nil {
			return err
		}
		if err = s.validatePRDeployment(ctx, connection, *review, app, record); err != nil {
			return err
		}
	}
	if _, err = s.Syncer.ApplyWithApprovals(ctx, record.ID, reviewActorID, ids); err != nil {
		return err
	}
	if !record.Plan.Decommission {
		review.Phase = "syncing"
	}
	return nil
}

func prPlanComment(review store.PullRequestReview, view planView, target string) string {
	var body strings.Builder
	fmt.Fprintf(&body, "### JustCD deployment plan\n\nCommit: `%s`\n\nPlan: `%s`\n\nDigest: `%s`\n\n", review.HeadSHA, view.ID, view.Plan.Digest)
	fmt.Fprintf(&body, "[Review the full plan](%s)\n\n", target)
	for i, change := range view.Plan.Changes {
		if i >= 50 {
			fmt.Fprintf(&body, "\n… %d more changes in JustCD.\n", len(view.Plan.Changes)-50)
			break
		}
		fmt.Fprintf(&body, "- %s: `%s` `%s/%s` (`%s`)\n", change.Kind, change.Identity.Kind, change.Identity.Namespace, change.Identity.Name, change.Identity.APIVersion)
	}
	if view.Status != "review-only" && core.RequiredApprovalCount(view.Plan) > 0 {
		fmt.Fprintf(&body, "\nApproval required. A mapped, eligible JustCD user can post:\n\n```text\n/justcd approve %s %s\n```\n\nApprovals apply only to this plan and digest, and expire with the plan.\n", view.ID, view.Plan.Digest)
	} else if view.Status == "review-only" {
		body.WriteString("\nReview only — this plan cannot be deployed.\n")
	} else {
		body.WriteString("\nAutomatic deployment is allowed by the deployment policy.\n")
	}
	return body.String()
}
