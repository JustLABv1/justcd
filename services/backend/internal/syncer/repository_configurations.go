package syncer

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/repoconfig"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// RunRepositoryPoller runs independently so Git discovery cannot delay drift checks.
func (s *Service) RunRepositoryPoller(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		s.reconcileRepositories(ctx, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) reconcileRepositories(ctx context.Context, logger *slog.Logger) {
	repositories, err := s.Store.ListRepositoryConfigurations(ctx, "", true)
	if err != nil {
		logger.ErrorContext(ctx, "could not list repository configurations", "error", err)
		return
	}
	for _, repository := range repositories {
		if ctx.Err() != nil {
			return
		}
		if err := s.ReconcileRepository(ctx, repository.ID); err != nil {
			logger.WarnContext(ctx, "repository configuration reconciliation failed", "repositoryId", repository.ID, "error", err)
		}
	}
}

func (s *Service) ReconcileRepository(parent context.Context, id string) (resultErr error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	// A session lock spans fetching and applying, including across backend replicas.
	conn, err := s.Store.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(7412,hashtext($1))`, id).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("repository discovery is already running")
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(7412,hashtext($1))`, id); err != nil {
			// Do not return a session with an unreleased advisory lock to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	repository, err := s.Store.RepositoryConfigurationByID(ctx, id)
	if err != nil {
		return err
	}
	if !repository.Enabled {
		return errors.New("repository discovery is paused")
	}
	defer func() {
		if resultErr != nil {
			recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.Store.RecordRepositoryConfigurationError(recordCtx, id, resultErr.Error())
		}
	}()
	source, err := s.Store.GitSourceForWorkspace(ctx, repository.SourceID, repository.WorkspaceID)
	if err != nil {
		return errors.New("repository Git source is unavailable to this workspace")
	}
	checkout, err := gitops.Fetch(ctx, s.Store, s.EncryptionKey, source, repository.Revision)
	if err != nil {
		return err
	}
	defer checkout.Close()
	definitions, err := repoconfig.Discover(checkout.Root)
	if err != nil {
		return err
	}
	clusters, err := s.Store.ListClusters(ctx, repository.WorkspaceID)
	if err != nil {
		return err
	}
	apps := make([]store.Application, 0, len(definitions))
	for _, definition := range definitions {
		clusterID := ""
		for _, cluster := range clusters {
			if cluster.ID == definition.Spec.Destination.Cluster || cluster.Name == definition.Spec.Destination.Cluster {
				if clusterID != "" && clusterID != cluster.ID {
					return fmt.Errorf("%s: cluster reference is ambiguous; use its ID", definition.File)
				}
				clusterID = cluster.ID
			}
		}
		if clusterID == "" {
			return fmt.Errorf("%s: cluster %q is unavailable to this workspace", definition.File, definition.Spec.Destination.Cluster)
		}
		binding, err := s.Store.NamespaceBinding(ctx, repository.WorkspaceID, clusterID, definition.Spec.Destination.Namespace)
		if err != nil {
			return fmt.Errorf("%s: namespace %q requires an existing workspace binding", definition.File, definition.Spec.Destination.Namespace)
		}
		apps = append(apps, store.Application{
			WorkspaceID: repository.WorkspaceID, Name: definition.Metadata.Name, SourceID: repository.SourceID, Revision: repository.Revision,
			ManifestPath: definition.Spec.Source.Path, Renderer: definition.Spec.Source.Renderer,
			HelmReleaseName: definition.Spec.Source.ReleaseName, HelmValuesFiles: definition.Spec.Source.ValuesFiles, HelmValuesYAML: definition.Spec.Source.ValuesYAML,
			KustomizeHelmEnabled: definition.Spec.Source.KustomizeHelmEnabled, KustomizeNamespaceOverride: definition.Spec.Source.KustomizeNamespaceOverride,
			ClusterID: clusterID, Namespaces: []store.NamespaceBinding{binding}, SyncPolicy: definition.Spec.SyncPolicy, PollSeconds: definition.Spec.PollSeconds, RetryPolicy: store.DefaultRetryPolicy(),
			RepositoryConfigurationID: id, ConfigurationPath: definition.File, ConfigurationHash: definition.Hash,
		})
		app := &apps[len(apps)-1]
		for _, ignore := range definition.Spec.IgnoreResources {
			if ignore.Name != "" {
				app.RepositoryIgnoreRules = append(app.RepositoryIgnoreRules, core.IgnoreRule{Identity: core.Identity{ClusterID: clusterID, APIVersion: ignore.APIVersion, Kind: ignore.Kind, Namespace: ignore.Namespace, Name: ignore.Name, ClusterScoped: ignore.ClusterScoped}, Reason: ignore.Reason, ManagedByGit: true})
			} else {
				app.RepositoryIgnoreSelectors = append(app.RepositoryIgnoreSelectors, core.IgnoreSelector{APIVersion: ignore.APIVersion, Kind: ignore.Kind, LabelKey: ignore.LabelKey, LabelValue: ignore.LabelValue, Reason: ignore.Reason, ManagedByGit: true})
			}
		}
	}
	if err := s.Store.ApplyRepositoryApplications(ctx, repository, checkout.Commit, apps); err != nil {
		return err
	}
	if repository.LastCommit != checkout.Commit || repository.LastError != "" {
		_ = s.Store.Audit(ctx, "", "repository.configuration.reconciled", "repository_configuration", id, map[string]any{"commit": checkout.Commit, "applications": len(apps), "workspaceId": repository.WorkspaceID, "actor": systemActorID})
	}
	return nil
}
