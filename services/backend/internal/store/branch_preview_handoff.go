package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (s *Store) AttachSharedPR(ctx context.Context, expected PullRequestReview, parent Application, revision string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	connection, err := scanSourceControlConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM source_control_connections WHERE id=$1 FOR UPDATE`, expected.ConnectionID))
	if err != nil {
		return err
	}
	if !connection.Enabled || !connection.PreviewProfile.Enabled || connection.PreviewProfile.DeploymentMode != "existing" || !connection.PreviewProfile.ConfirmShared || connection.PreviewProfile.AllowForks || connection.ApplicationID != parent.ID {
		return errors.New("shared PR deployments are not authorized")
	}
	review, err := scanReview(tx.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM pull_request_reviews WHERE id=$1 FOR UPDATE`, expected.ID))
	if err != nil {
		return err
	}
	if review.Closed || review.Fork || review.HeadSHA != expected.HeadSHA || review.PreviewApplicationID != nil {
		return errors.New("PR changed or already attached")
	}
	app, err := branchTestApplication(ctx, tx, parent.ID)
	if err != nil {
		return err
	}
	if app.BranchTest != nil || app.RollbackResumeState != nil || app.ApplicationGroupID != "" || app.SourceID != parent.SourceID || app.Revision != parent.Revision || len(app.Namespaces) == 0 {
		return errors.New("finish the current branch test or rollback before deploying a PR here")
	}
	state := BranchTest{Mode: "existing", Revision: revision, BaseRevision: app.Revision, PreviouslyPaused: app.AutoSyncPaused, StartedAt: time.Now().UTC()}
	raw, _ := json.Marshal(state)
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET revision=$2,branch_test=$3,auto_sync_paused=TRUE,retry_next_at=NULL,updated_at=NOW() WHERE id=$1`, app.ID, revision, raw); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, app.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pull_request_reviews SET preview_application_id=$2,shared_environment=TRUE WHERE id=$1`, review.ID, app.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MatchingBranchPreviews(ctx context.Context, parent Application, review PullRequestReview) ([]Application, error) {
	apps, err := s.ListApplications(ctx, parent.WorkspaceID)
	if err != nil {
		return nil, err
	}
	items := []Application{}
	if review.Fork || review.Closed || review.HeadBranch == "" {
		return items, nil
	}
	for _, app := range apps {
		b := app.BranchTest
		if b != nil && b.Mode == "isolated" && b.ParentApplicationID == parent.ID && b.Revision == review.HeadBranch && app.SourceID == parent.SourceID && app.ClusterID == parent.ClusterID && !app.Decommissioning && len(app.Namespaces) == 1 {
			var attached bool
			if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pull_request_reviews WHERE preview_application_id=$1)`, app.ID).Scan(&attached); err != nil {
				return nil, err
			}
			if !attached {
				items = append(items, app)
			}
		}
	}
	return items, nil
}

// The handoff invalidates manual plans and preserves resource/application IDs.
// Namespace ownership is deliberately not transferred: cleanup retains it.
func (s *Store) AdoptBranchPreview(ctx context.Context, expected PullRequestReview, parent Application, previewID, revision string, maxActive int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	connection, err := scanSourceControlConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM source_control_connections WHERE id=$1 FOR UPDATE`, expected.ConnectionID))
	if err != nil {
		return err
	}
	if !connection.Enabled || !connection.PreviewProfile.Enabled || connection.PreviewProfile.DeploymentMode == "existing" || connection.ApplicationID != parent.ID {
		return errors.New("isolated PR previews are not enabled")
	}
	maxActive = connection.PreviewProfile.MaxActive
	review, err := scanReview(tx.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM pull_request_reviews WHERE id=$1 FOR UPDATE`, expected.ID))
	if err != nil {
		return err
	}
	if review.Closed || review.Fork || review.HeadSHA != expected.HeadSHA || review.HeadBranch != expected.HeadBranch || review.PreviewApplicationID != nil || review.Phase != "adoption_available" {
		return errors.New("PR changed or already has a preview; refresh before choosing")
	}
	var working bool
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(lease_until>NOW(),FALSE) FROM pull_request_reviews WHERE id=$1`, review.ID).Scan(&working); err != nil {
		return err
	}
	if working {
		return errors.New("PR worker is still updating this review; retry the preview selection shortly")
	}
	if previewID == "" {
		_, err = tx.ExecContext(ctx, `UPDATE pull_request_reviews SET branch_preview_declined=TRUE,phase='pending',reported_phase='',updated_at=NOW() WHERE id=$1`, review.ID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	app, err := branchTestApplication(ctx, tx, previewID)
	if err != nil {
		return err
	}
	b := app.BranchTest
	if b == nil || b.Mode != "isolated" || b.ParentApplicationID != parent.ID || b.Revision != review.HeadBranch || app.WorkspaceID != parent.WorkspaceID || app.SourceID != parent.SourceID || app.ClusterID != parent.ClusterID || len(app.Namespaces) != 1 {
		return errors.New("branch preview does not match this PR")
	}
	var slots int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM preview_slots WHERE connection_id=$1`, expected.ConnectionID).Scan(&slots); err != nil {
		return err
	}
	if slots >= maxActive {
		return errors.New("maximum active previews reached")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO preview_slots(connection_id,number) VALUES($1,$2)`, expected.ConnectionID, review.Number); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET revision=$2,branch_test=NULL,auto_sync_paused=TRUE,updated_at=NOW() WHERE id=$1`, app.ID, revision); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, app.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pull_request_reviews SET preview_application_id=$2,adopted_branch_preview=TRUE,phase='pending',processed_sha='',reported_phase='',updated_at=NOW() WHERE id=$1`, review.ID, app.ID); err != nil {
		return err
	}
	return tx.Commit()
}
