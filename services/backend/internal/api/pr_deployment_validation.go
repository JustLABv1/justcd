package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) reviewPlanNeedsRefresh(ctx context.Context, review store.PullRequestReview) bool {
	if review.Phase != "approval_required" {
		return false
	}
	var view planView
	if json.Unmarshal(review.Plan, &view) != nil {
		return true
	}
	plan, err := s.Store.PlanByID(ctx, view.ID)
	return err != nil || plan.Status == "stale" || (plan.Status == "current" && !plan.ExpiresAt.After(time.Now()))
}

func (s *Server) validatePRDeployment(ctx context.Context, connection store.SourceControlConnection, review store.PullRequestReview, app store.Application, record store.PlanRecord) error {
	if !connection.Enabled || review.Closed || record.Plan.Revision != review.HeadSHA {
		return errors.New("PR is closed or no longer matches this plan")
	}
	if !review.SharedEnvironment {
		_, namespace := previewIdentity(connection.ID, review.Number, connection.PreviewProfile.NamespacePrefix)
		if review.AdoptedBranchPreview && len(app.Namespaces) == 1 {
			namespace = app.Namespaces[0].Namespace
		}
		if review.ExpiresAt == nil || !review.ExpiresAt.After(time.Now()) || len(app.Namespaces) != 1 || app.Namespaces[0].Namespace != namespace {
			return errors.New("preview is expired or its destination changed")
		}
		_, desired, err := s.Syncer.CalculatePlanWithSelection(ctx, app, record.Plan.Selection)
		if err != nil {
			return err
		}
		if err = validatePreviewResources(desired, connection.PreviewProfile, namespace, review.Fork); err != nil {
			return err
		}
	} else if review.Fork || app.ID != connection.ApplicationID || app.BranchTest == nil || app.BranchTest.Mode != "existing" {
		return errors.New("shared PR deployment target changed")
	}
	token, err := s.sourceControlToken(ctx, connection)
	if err != nil {
		return err
	}
	current, err := (scm.Client{}).Current(ctx, connection.Provider, connection.APIURL, connection.Repository, string(token), review.Number)
	if err != nil {
		return err
	}
	if current.Closed || current.HeadSHA != record.Plan.Revision || current.Fork != review.Fork {
		return errors.New("PR changed before deployment; review a fresh plan")
	}
	return nil
}
