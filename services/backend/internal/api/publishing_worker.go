package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/security"
)

// Leases and results live in PostgreSQL so restarts and multiple API replicas can
// safely resume completion. Remote writes are fast-forward-only and idempotent.
func (s *Server) RunPublishingWorker(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		s.pollPublishingCompletions(ctx, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) pollPublishingCompletions(ctx context.Context, logger *slog.Logger) {
	rows, err := s.Store.DB.QueryContext(ctx, `WITH due AS (SELECT application_id FROM published_applications WHERE config->>'managedCompletion'='true' AND pipeline_url<>'' AND completion_next_at<=now() ORDER BY completion_next_at LIMIT 10 FOR UPDATE SKIP LOCKED) UPDATE published_applications p SET completion_next_at=now()+interval '3 minutes' FROM due WHERE p.application_id=due.application_id RETURNING p.application_id`)
	if err != nil {
		logger.Warn("could not list publishing completions")
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		work, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := s.completePublication(work, id)
		cancel()
		if err != nil {
			logger.Warn("publishing completion needs attention", "applicationId", id, "error", err)
			_, _ = s.Store.DB.ExecContext(ctx, `UPDATE published_applications SET completion_status='retrying',completion_error=$2,completion_next_at=now()+interval '30 seconds' WHERE application_id=$1`, id, err.Error())
		}
	}
}
func (s *Server) completePublication(ctx context.Context, id string) error {
	app, err := s.Store.ApplicationByID(ctx, id)
	if err != nil {
		return err
	}
	if app.Decommissioning {
		return errors.New("publication is decommissioning")
	}
	src, err := s.Store.GitSourceByID(ctx, app.SourceID)
	if err != nil {
		return err
	}
	p, err := s.Store.PublishedApplication(ctx, app.WorkspaceID, src.RepositoryURL)
	if err != nil {
		return err
	}
	if p.ApplicationID != id || app.Revision != "main" || app.ManifestPath != "deploy" || src.WorkspaceID != app.WorkspaceID {
		return errors.New("application no longer matches managed publishing source")
	}
	var cfg publishing.Config
	if json.Unmarshal(p.Config, &cfg) != nil || !cfg.ManagedCompletion {
		return errors.New("publication is not managed by completion worker")
	}
	var token string
	if p.ProfileID != nil {
		profile, e := s.Store.PublishingProfile(ctx, *p.ProfileID)
		if e != nil || profile.WorkspaceID != app.WorkspaceID || !profile.Enabled {
			return errors.New("publishing profile is unavailable or disabled")
		}
		secrets, e := s.loadPublishingSecrets((&http.Request{}).WithContext(ctx), profile)
		if e != nil {
			return e
		}
		token = secrets.GitLabToken
	} else {
		var cipher []byte
		if err = s.Store.DB.QueryRowContext(ctx, `SELECT completion_token_cipher FROM published_applications WHERE application_id=$1`, id).Scan(&cipher); err != nil {
			return err
		}
		plain, e := security.Decrypt(s.EncryptionKey, cipher, "publication:"+id)
		if e != nil {
			return errors.New("publishing write credential is unavailable; repeat setup to refresh it")
		}
		token = string(plain)
	}
	git := publishing.GitLab{URL: p.GitLabURL, Token: token, HTTP: s.PublishingHTTP}
	if err = git.Validate(); err != nil {
		return err
	}
	var completed int64
	if err = s.Store.DB.QueryRowContext(ctx, `SELECT completion_pipeline_id FROM published_applications WHERE application_id=$1`, id).Scan(&completed); err != nil {
		return err
	}
	build, digest, err := git.SuccessfulBuild(ctx, p.ProjectID, completed)
	if err != nil {
		return err
	}
	if build.ID == completed && completed > 0 {
		_, err = s.Store.DB.ExecContext(ctx, `UPDATE published_applications SET completion_status='completed',completion_error='',completion_next_at=now()+interval '30 seconds' WHERE application_id=$1`, id)
		return err
	}
	if digest == "" {
		_, err = s.Store.DB.ExecContext(ctx, `UPDATE published_applications SET completion_status=$2,completion_error='',completion_next_at=now()+interval '30 seconds' WHERE application_id=$1`, id, "build_"+build.Status)
		return err
	}
	// Hold the same per-repository lock used by uploads and upgrades until the push
	// finishes, so managed settings cannot change between verification and commit.
	marker := publishingMarker(app.WorkspaceID, p.GitLabURL, p.Group, cfg.Name)
	lock, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer lock.Rollback()
	var acquired bool
	if err = lock.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, marker).Scan(&acquired); err != nil {
		return err
	}
	if !acquired {
		return errors.New("publication is being updated; retrying completion")
	}
	fresh, err := s.Store.PublishedApplication(ctx, app.WorkspaceID, p.RepositoryURL)
	if err != nil {
		return err
	}
	if string(fresh.Config) != string(p.Config) || fresh.Fingerprint != p.Fingerprint {
		return errors.New("publication changed while build was inspected")
	}
	// Prove repository ownership and identity before forwarding its write token.
	if err = git.VerifyManagedProject(ctx, p.ProjectID, p.RepositoryURL, marker); err != nil {
		return err
	}
	if err = publishing.CommitDeployment(ctx, p.RepositoryURL, token, build.SHA, digest, cfg.RegistryImage, build.ID); err != nil {
		return err
	}
	_, err = s.Store.DB.ExecContext(ctx, `UPDATE published_applications SET completion_pipeline_id=$2,completion_status='completed',completion_error='',completion_next_at=now()+interval '30 seconds' WHERE application_id=$1`, id, build.ID)
	if err == nil {
		_ = s.Store.Audit(ctx, "justcd-system", "application.build_completed", "application", id, map[string]any{"pipelineId": build.ID, "sourceSha": build.SHA, "imageDigest": digest})
	}
	return err
}

func publishingMarker(workspace, origin, group, name string) string {
	sum := sha256.Sum256([]byte(workspace + "\n" + strings.TrimRight(origin, "/") + "\n" + group + "/" + name))
	return "JustCD managed publication " + hex.EncodeToString(sum[:])
}
