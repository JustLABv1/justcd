package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/repoconfig"
	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
)

func (s *Server) repositoryPRConnection(ctx context.Context, repo store.RepositoryConfiguration) (store.SourceControlConnection, error) {
	source, err := s.Store.GitSourceForWorkspace(ctx, repo.SourceID, repo.WorkspaceID)
	if err != nil {
		return store.SourceControlConnection{}, err
	}
	d, err := scm.Details(source.RepositoryURL, repo.PRSettings.Provider, repo.PRSettings.APIURL)
	return store.SourceControlConnection{CommentMode: repo.PRSettings.CommentMode, CommentOnDrafts: repo.PRSettings.CommentOnDrafts, PipelineStatusReporting: repo.PRSettings.PipelineStatusReporting, Provider: d.Provider, APIURL: d.APIURL, Repository: d.Repository, WorkspaceID: repo.WorkspaceID, StatusCredentialID: &repo.PRSettings.CredentialID, Enabled: true, ManagedByGit: true}, err
}
func (s *Server) listRepositoryPRApplications(w http.ResponseWriter, r *http.Request) {
	repo, err := s.Store.RepositoryConfigurationByID(r.Context(), r.PathValue("repositoryID"))
	if err != nil {
		writeError(w, 404, "repository not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, repo.WorkspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListRepositoryPRApplications(r.Context(), repo.ID)
	if err != nil {
		writeStoreError(w, "could not list repository PR applications")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) saveRepositoryPRSettings(w http.ResponseWriter, r *http.Request) {
	repo, err := s.Store.RepositoryConfigurationByID(r.Context(), r.PathValue("repositoryID"))
	if err != nil {
		writeError(w, 404, "repository not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, repo.WorkspaceID, "owner") {
		return
	}
	var p store.RepositoryPRSettings
	if err = decodeJSON(w, r, &p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.Store.SaveRepositoryPRSettings(r.Context(), repo.ID, p); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "repository.pr_policy.updated", "repository_configuration", repo.ID, map[string]any{"mode": p.Mode, "enabled": p.Enabled, "destinations": p.Destinations})
	repo, err = s.Store.RepositoryConfigurationByID(r.Context(), repo.ID)
	if err != nil {
		writeStoreError(w, "could not reload repository")
		return
	}
	writeJSON(w, 200, repo)
}
func (s *Server) pollRepositoryPRs(ctx context.Context, logger *slog.Logger) {
	repos, err := s.Store.EnabledPRRepositories(ctx)
	if err != nil {
		logger.Warn("could not list repository PR policies", "error", err)
		return
	}
	for _, repo := range repos {
		work, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err = s.discoverRepositoryPRs(work, repo)
		cancel()
		message := ""
		if err != nil {
			message = err.Error()
			logger.Warn("repository PR discovery failed", "repositoryId", repo.ID, "error", err)
		}
		_, _ = s.Store.DB.ExecContext(ctx, `UPDATE repository_configurations SET pr_error=$2 WHERE id=$1`, repo.ID, message)
	}
}
func (s *Server) discoverRepositoryPRs(ctx context.Context, repo store.RepositoryConfiguration) error {
	// Serialize the complete scan across replicas; application workers may still run.
	db, err := s.Store.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	var locked bool
	if err = db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(7413,hashtext($1))`, repo.ID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cleanup, `SELECT pg_advisory_unlock(7413,hashtext($1))`, repo.ID)
	}()
	c, err := s.repositoryPRConnection(ctx, repo)
	if err != nil {
		return err
	}
	token, err := s.sourceControlToken(ctx, c)
	if err != nil {
		return err
	}
	client := scm.Client{}
	open, err := client.ListOpen(ctx, c.Provider, c.APIURL, c.Repository, string(token))
	if err != nil {
		return err
	}
	source, err := s.Store.GitSourceForWorkspace(ctx, repo.SourceID, repo.WorkspaceID)
	if err != nil {
		return err
	}
	base, err := gitops.Fetch(ctx, s.Store, s.EncryptionKey, source, repo.Revision)
	if err != nil {
		return err
	}
	defer base.Close()
	baseline, err := repoconfig.Discover(base.Root)
	if err != nil {
		return err
	}
	clusters, err := s.Store.ListClusters(ctx, repo.WorkspaceID)
	if err != nil {
		return err
	}
	tracked := map[string]bool{}
	for _, d := range baseline {
		clusterID, err := repositoryPRClusterID(clusters, d.Spec.Destination.Cluster)
		if err != nil {
			return err
		}
		if clusterID != "" {
			tracked[clusterID+"\x00"+d.Metadata.Name] = true
		}
	}
	known, err := s.Store.ListRepositoryPRApplications(ctx, repo.ID)
	if err != nil {
		return err
	}
	var scanErrors []error
	for _, event := range open {
		if event.Fork || event.TargetBranch != strings.TrimPrefix(repo.Revision, "refs/heads/") {
			continue
		}
		err := func() error {
			changedPaths, filesErr := client.ChangedPaths(ctx, c.Provider, c.APIURL, c.Repository, string(token), event.Number)
			knownApplication := false
			for _, v := range known {
				if v.Number == event.Number && v.ApplicationID != nil && v.Phase != "promoted" {
					knownApplication = true
				}
			}
			if filesErr == nil && !knownApplication && !repositoryPRHasDefinitions(changedPaths) {
				return nil
			}
			checkout, err := gitops.Fetch(ctx, s.Store, s.EncryptionKey, source, pullRequestRef(c.Provider, event.Number))
			if err != nil {
				return err
			}
			definitions, discoverErr := repoconfig.Discover(checkout.Root)
			sha := checkout.Commit
			checkout.Close()
			if discoverErr != nil {
				return discoverErr
			}
			if sha != event.HeadSHA {
				return nil
			}
			current, err := client.Current(ctx, c.Provider, c.APIURL, c.Repository, string(token), event.Number)
			if err != nil {
				return err
			}
			if current.Closed || current.Fork || current.HeadSHA != sha || current.TargetBranch != event.TargetBranch {
				return nil
			}
			present := map[string]bool{}
			for _, definition := range definitions {
				clusterID, err := repositoryPRClusterID(clusters, definition.Spec.Destination.Cluster)
				if err != nil {
					return err
				}
				if clusterID == "" {
					continue
				}
				key := clusterID + "\x00" + definition.Metadata.Name
				if present[key] {
					return fmt.Errorf("duplicate application name %q for cluster %q", definition.Metadata.Name, clusterID)
				}
				present[key] = true
				if tracked[key] {
					continue
				}
				app, eligible, err := s.resolveRepositoryPRDefinition(ctx, repo, definition, clusterID)
				if err != nil {
					return err
				}
				if !eligible {
					continue
				}
				conn := c
				conn.ID = store.NewID()
				for _, v := range known {
					if v.Number == event.Number && v.DefinitionName == app.Name && v.ClusterID == app.ClusterID && v.ConnectionID != nil {
						conn.ID = *v.ConnectionID
					}
				}
				conn.PreviewProfile = repo.PRSettings.Profile
				conn.PreviewProfile.Enabled = repo.PRSettings.Mode != "review-only"
				conn.PreviewProfile.DeploymentMode = repo.PRSettings.Mode
				conn.PreviewProfile.AllowForks = false
				originalName := app.Name
				app.RepositoryConfigurationID = ""
				app.RepositoryPullRequests = nil
				app.Revision = sha
				app.SyncPolicy = "manual"
				app.AutoSyncPaused = true
				app.PollSeconds = 86400
				if repo.PRSettings.Mode != "existing" {
					app.Name = fmt.Sprintf("%s-mr-%d-%s", app.Name, event.Number, conn.ID[:8])
					if len(app.Name) > 100 {
						app.Name = app.Name[:100]
					}
				}
				if repo.PRSettings.Mode == "isolated" {
					_, ns := previewIdentity(conn.ID, event.Number, conn.PreviewProfile.NamespacePrefix)
					binding, e := s.Store.NamespaceBinding(ctx, repo.WorkspaceID, app.ClusterID, ns)
					if errors.Is(e, sql.ErrNoRows) {
						e = s.Store.CreateNamespaceBinding(ctx, repo.WorkspaceID, app.ClusterID, ns, nil)
						binding = store.NamespaceBinding{Namespace: ns}
					}
					if e != nil {
						return e
					}
					app.Namespaces = []store.NamespaceBinding{binding}
					app.KustomizeNamespaceOverride = app.Renderer == "kustomize"
					app.HelmReleaseName = ""
				}
				v := store.RepositoryPRApplication{RepositoryID: repo.ID, Number: event.Number, DefinitionName: originalName, ConfigurationPath: definition.File, Mode: repo.PRSettings.Mode}
				v, err = s.Store.UpsertRepositoryPRApplication(ctx, repo, v, app, conn)
				if err != nil {
					return err
				}
				if v.ConnectionID == nil || v.Phase == "promoted" {
					continue
				}
				if err = s.refreshRepositoryPREvent(ctx, *v.ConnectionID, current); err != nil {
					return err
				}
				if app.CreateNamespaces {
					if err := s.Syncer.BootstrapRepositoryPRNamespaces(ctx, *v.ApplicationID); err != nil {
						return err
					}
				}
			}
			// Removed definitions enter the same reviewed cleanup flow as a closed MR.
			for _, v := range known {
				if v.Number == event.Number && v.ConnectionID != nil && v.ApplicationID != nil && !present[v.ClusterID+"\x00"+v.DefinitionName] {
					_, _ = s.Store.DB.ExecContext(ctx, `UPDATE repository_pr_applications SET definition_removed=TRUE,phase='definition_removed' WHERE id=$1`, v.ID)
					_, _ = s.Store.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET phase='pending',updated_at=NOW() WHERE connection_id=$1`, *v.ConnectionID)
				}
			}
			return nil
		}()
		if err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("PR #%d: %w", event.Number, err))
			if reportErr := s.reportRepositoryPRDiscoveryFailure(ctx, client, c, repo, event, string(token), err); reportErr != nil {
				scanErrors = append(scanErrors, fmt.Errorf("PR #%d: could not post discovery failure comment: %w", event.Number, reportErr))
			}
		} else if c.CommentMode != "off" && (!event.Draft || c.CommentOnDrafts) {
			if reportErr := s.Store.UpdatePRComment(ctx, client, c, event.Number, string(token), "discovery:"+repo.ID, "", false); reportErr != nil {
				scanErrors = append(scanErrors, reportErr)
			}
		}
	}
	for _, v := range known {
		if v.ConnectionID == nil || v.ApplicationID == nil || v.Phase == "promoted" {
			continue
		}
		current, err := client.Current(ctx, c.Provider, c.APIURL, c.Repository, string(token), v.Number)
		if err != nil {
			scanErrors = append(scanErrors, err)
			continue
		}
		if err = s.refreshRepositoryPREvent(ctx, *v.ConnectionID, current); err != nil {
			scanErrors = append(scanErrors, err)
			continue
		}
	}

	return errors.Join(scanErrors...)
}

func (s *Server) refreshRepositoryPREvent(ctx context.Context, connectionID string, event scm.Event) error {
	reviews, err := s.Store.ListReviews(ctx, connectionID)
	if err != nil {
		return err
	}
	for _, r := range reviews {
		if r.Number == event.Number && r.HeadSHA == event.HeadSHA && r.Closed == event.Closed && r.HeadBranch == event.HeadBranch && r.Fork == event.Fork && r.Draft == event.Draft {
			return nil
		}
	}
	// A fresh authoritative read must supersede a locally registered placeholder.
	event.EventAt = time.Now().UTC()
	return s.recordPolledReview(ctx, connectionID, event)
}

// Only a complete provider response may rule out new application definitions.
func repositoryPRHasDefinitions(paths []string) bool {
	for _, filename := range paths {
		base := path.Base(filename)
		if base == "justcd.yaml" || base == "justcd.yml" {
			return true
		}
	}
	return false
}

// Destination rules select PR applications to discover. Excluded definitions
// must not require namespace bindings or create application/namespace records.
func (s *Server) resolveRepositoryPRDefinition(ctx context.Context, repo store.RepositoryConfiguration, definition repoconfig.Definition, clusterID string) (store.Application, bool, error) {
	if !repo.PRSettings.Allows(clusterID, definition.Spec.Destination.Namespace) {
		return store.Application{}, false, nil
	}
	// Branch YAML may request creation only within the owner's policy.
	definition.Spec.Destination.CreateNamespaces = definition.Spec.Destination.CreateNamespaces && repo.PRSettings.AllowNamespaceCreation && repo.PRSettings.Mode == "existing"
	app, err := s.Syncer.ResolveRepositoryDefinition(ctx, repo, definition)
	if err != nil {
		if strings.Contains(err.Error(), "requires an existing workspace binding") {
			return store.Application{}, true, repositoryPRBindingError(repo.PRSettings.Mode, definition.File, definition.Spec.Destination.Namespace)
		}
		return store.Application{}, true, err
	}
	return app, true, nil
}

// Resolve identity without requiring deployment access to every namespace in a
// PR's complete snapshot. An unavailable cluster cannot match an allowed rule.
func repositoryPRClusterID(clusters []store.Cluster, reference string) (string, error) {
	clusterID := ""
	for _, cluster := range clusters {
		if cluster.ID == reference || cluster.Name == reference {
			if clusterID != "" && clusterID != cluster.ID {
				return "", fmt.Errorf("cluster reference %q is ambiguous; use its ID", reference)
			}
			clusterID = cluster.ID
		}
	}
	return clusterID, nil
}
