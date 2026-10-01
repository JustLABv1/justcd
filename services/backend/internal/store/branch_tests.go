package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// BranchTest identifies a temporary source override or an isolated manual preview.
type BranchTest struct {
	Mode                string    `json:"mode"`
	Revision            string    `json:"revision"`
	BaseRevision        string    `json:"baseRevision"`
	ParentApplicationID string    `json:"parentApplicationId,omitempty"`
	PreviouslyPaused    bool      `json:"previouslyPaused"`
	StartedAt           time.Time `json:"startedAt"`
}

func branchTestApplication(ctx context.Context, tx *sql.Tx, id string) (Application, error) {
	app, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return app, err
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return app, err
	}
	if active {
		return app, errors.New("application is currently syncing; wait for the operation to finish")
	}
	if app.Decommissioning || app.ConfigurationMissing {
		return app, errors.New("application is being removed or its Git definition is missing")
	}
	return app, nil
}

func (s *Store) StartBranchTest(ctx context.Context, expected Application, revision, mode, namespace, manifestPath string) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	app, err := branchTestApplication(ctx, tx, expected.ID)
	if err != nil {
		return "", err
	}
	if app.SourceID != expected.SourceID || app.Revision != expected.Revision || app.ConfigurationHash != expected.ConfigurationHash {
		return "", errors.New("application source changed; refresh before testing a branch")
	}
	if app.BranchTest != nil || app.RollbackResumeState != nil {
		return "", errors.New("resume the current branch test or rollback pin before starting another test")
	}
	if app.ApplicationGroupID != "" {
		return "", errors.New("branch testing requires a standalone application; application group targets are not supported")
	}
	var reviewPreview bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pull_request_reviews WHERE preview_application_id=$1)`, app.ID).Scan(&reviewPreview); err != nil {
		return "", err
	}
	if reviewPreview {
		return "", errors.New("pull request previews must be managed through their pull request review")
	}
	state := BranchTest{Mode: mode, Revision: revision, BaseRevision: app.Revision, PreviouslyPaused: app.AutoSyncPaused, StartedAt: time.Now().UTC()}
	id := app.ID
	switch mode {
	case "existing":
		raw, _ := json.Marshal(state)
		if _, err = tx.ExecContext(ctx, `UPDATE applications SET revision=$2,branch_test=$3,auto_sync_paused=TRUE,retry_next_at=NULL,retry_attempt_count=0,retry_terminal_reason='',retry_last_error_code='',last_checked_at=NULL,updated_at=NOW() WHERE id=$1`, id, revision, raw); err != nil {
			return "", err
		}
	case "isolated":
		if namespace == "" || manifestPath == "" {
			return "", errors.New("an isolated namespace and preview manifest path are required")
		}
		var used bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM namespace_bindings WHERE cluster_id=$1 AND namespace=$2) OR EXISTS(SELECT 1 FROM applications a,jsonb_array_elements(a.namespaces) binding WHERE a.cluster_id=$1 AND binding->>'namespace'=$2)`, app.ClusterID, namespace).Scan(&used); err != nil {
			return "", err
		}
		if used {
			return "", errors.New("preview namespace is already registered; choose a dedicated unused namespace")
		}
		binding, err := ensureRepositoryNamespaceBinding(ctx, tx, app.WorkspaceID, app.ClusterID, namespace, true)
		if err != nil {
			return "", err
		}
		state.ParentApplicationID = app.ID
		app.ID = NewID()
		id = app.ID
		app.Name = namespace
		app.Revision = revision
		app.ManifestPath = manifestPath
		app.RepositoryConfigurationID = ""
		app.ApplicationGroupID = ""
		app.ConfigurationPath = ""
		app.ConfigurationHash = ""
		app.Namespaces = []NamespaceBinding{binding}
		app.CreateNamespaces = true
		app.SyncPolicy = "manual"
		app.TargetManifestPath = ""
		app.NamespaceManifestPaths = nil
		app.TargetHelmValuesFiles = nil
		app.TargetHelmValuesYAML = ""
		app.NamespaceHelmValues = nil
		app.KustomizeNamespaceOverride = app.Renderer == "kustomize"
		if err = createApplication(ctx, tx, app); err != nil {
			return "", err
		}
		raw, _ := json.Marshal(state)
		if _, err = tx.ExecContext(ctx, `UPDATE applications SET branch_test=$2,auto_sync_paused=TRUE WHERE id=$1`, id, raw); err != nil {
			return "", err
		}
	default:
		return "", errors.New("mode must be existing or isolated")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, id); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *Store) ResumeBranchTest(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	app, err := branchTestApplication(ctx, tx, id)
	if err != nil {
		return err
	}
	if app.BranchTest == nil || app.BranchTest.Mode != "existing" {
		return errors.New("this application has no existing-environment branch test to resume")
	}
	revision := app.BranchTest.BaseRevision
	if app.RepositoryConfigurationID != "" {
		var enabled bool
		if err = tx.QueryRowContext(ctx, `SELECT revision,enabled FROM repository_configurations WHERE id=$1`, app.RepositoryConfigurationID).Scan(&revision, &enabled); err != nil {
			return err
		}
		if !enabled {
			return errors.New("repository discovery is paused; enable it before resuming its tracked source")
		}
	}
	// Keep reconciliation paused until the owner has reviewed the return plan.
	// Repository discovery may now restore the latest definition from its tracked branch.
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET revision=$2,branch_test=NULL,auto_sync_paused=TRUE,configuration_hash=CASE WHEN repository_configuration_id IS NOT NULL THEN '' ELSE configuration_hash END,retry_next_at=NULL,retry_attempt_count=0,retry_terminal_reason='',retry_last_error_code='',last_checked_at=NULL,updated_at=NOW() WHERE id=$1`, id, revision); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, id); err != nil {
		return err
	}
	return tx.Commit()
}
