//go:build integration

package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/core"
	"strings"
	"testing"
	"time"
)

func testRepositoryPRDiscovery(t *testing.T, ctx context.Context, s *Store) {
	repo := RepositoryConfiguration{ID: "bootstrap-repo", WorkspaceID: "integration-workspace", SourceID: "integration-git", Revision: "bootstrap-main", Enabled: true}
	if err := s.CreateRepositoryConfiguration(ctx, repo); err != nil {
		t.Fatal(err)
	}
	policy := RepositoryPRSettings{Enabled: true, Provider: "gitlab", CredentialID: "integration-git-credential", Mode: "existing", Destinations: []PRDestination{{ClusterID: "integration-cluster", Namespace: "default"}}, Profile: PreviewProfile{ConfirmShared: true, MaxActive: 10}}
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
		t.Fatal(err)
	}
	// A pattern can cover future bindings without granting access to another
	// cluster. Verify it survives persistence, then use it for registration.
	policy.Destinations = []PRDestination{{ClusterID: "integration-cluster", NamespacePattern: "default|dev-.*"}}
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
		t.Fatal(err)
	}
	unavailable := policy
	unavailable.Destinations = []PRDestination{{ClusterID: "unavailable-cluster", NamespacePattern: ".*"}}
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, unavailable); err == nil {
		t.Fatal("regex granted access to an unavailable cluster")
	}
	repo, err := s.RepositoryConfigurationByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.PRSettings.Allows("integration-cluster", "dev-future") || repo.PRSettings.Allows("integration-cluster", "production") {
		t.Fatal("persisted regex policy lost its namespace restriction")
	}
	binding, err := s.NamespaceBinding(ctx, repo.WorkspaceID, "integration-cluster", "default")
	if err != nil {
		t.Fatal(err)
	}
	app := Application{WorkspaceID: repo.WorkspaceID, Name: "new-mr-app", SourceID: repo.SourceID, Revision: strings.Repeat("a", 40), ManifestPath: "new-app", Renderer: "kustomize", ClusterID: "integration-cluster", Namespaces: []NamespaceBinding{binding}, SyncPolicy: "manual", PollSeconds: 86400, RetryPolicy: DefaultRetryPolicy(), ConfigurationPath: "new-app/justcd.yaml", ConfigurationHash: "one"}
	c := SourceControlConnection{ID: NewID(), Provider: "gitlab", APIURL: "https://gitlab.com/api/v4", Repository: "repo", StatusCredentialID: &policy.CredentialID, PreviewProfile: PreviewProfile{Enabled: true, DeploymentMode: "existing", ConfirmShared: true}}
	v := RepositoryPRApplication{RepositoryID: repo.ID, Number: 42, DefinitionName: app.Name, ConfigurationPath: app.ConfigurationPath, Mode: "existing"}
	v, err = s.UpsertRepositoryPRApplication(ctx, repo, v, app, c)
	if err != nil {
		t.Fatal(err)
	}
	id := *v.ApplicationID
	registered, err := s.ApplicationByID(ctx, id)
	if err != nil || !registered.AutoSyncPaused || registered.RepositoryConfigurationID != "" {
		t.Fatalf("registration incorrect: %+v %v", registered, err)
	}
	r, connection, err := s.ReviewByPreviewApplication(ctx, id)
	if err != nil || connection.RepositoryPRID == nil {
		t.Fatal("deployment guard not registered atomically", err)
	}
	// Same head is idempotent, and a new head updates the same application.
	again, err := s.UpsertRepositoryPRApplication(ctx, repo, v, app, c)
	if err != nil || *again.ApplicationID != id {
		t.Fatal("registration not idempotent", err)
	}
	app.Revision = strings.Repeat("b", 40)
	app.ConfigurationHash = "two"
	again, err = s.UpsertRepositoryPRApplication(ctx, repo, v, app, c)
	if err != nil || again.HeadSHA != app.Revision {
		t.Fatal("new commit not registered", err)
	}
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, RepositoryPRSettings{}); err == nil {
		t.Fatal("active policy changed")
	}
	// Reporting can be changed without closing active previews.
	for _, enabled := range []bool{true, false} {
		policy.PipelineStatusReporting = enabled
		policy.CommentMode = "action-required"
		policy.CommentOnDrafts = enabled
		if err := s.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
			t.Fatal(err)
		}
		updated, err := s.SourceControlConnectionByID(ctx, connection.ID)
		if err != nil || updated.PipelineStatusReporting != enabled || updated.CommentMode != "action-required" || updated.CommentOnDrafts != enabled {
			t.Fatalf("active preview reporting not updated: %+v, %v", updated, err)
		}
	}
	// A delayed provider event can update the placeholder; the app pointer survives.
	_, err = s.RecordReviewEvent(ctx, connection.ID, "first-provider-event", PullRequestReview{ID: NewID(), Number: 42, HeadSHA: app.Revision, EventAt: time.Now(), HeadBranch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	r, _, err = s.ReviewByPreviewApplication(ctx, id)
	if err != nil || r.HeadBranch != "feature" {
		t.Fatal("placeholder event did not refresh", err)
	}
	resource := core.Resource{Identity: core.Identity{ClusterID: app.ClusterID, APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "owned"}, Manifest: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"owned","namespace":"default"}}`)}
	if err = s.UpsertManagedResource(ctx, id, resource); err != nil {
		t.Fatal(err)
	}
	// Discovery cannot adopt an unmerged PR, or another manual application.
	tracked := app
	tracked.Revision = repo.Revision
	tracked.SyncPolicy = "auto-safe"
	tracked.ConfigurationHash = "merged"
	if err = s.ApplyRepositoryApplications(ctx, repo, "merge", []Application{tracked}); err == nil {
		t.Fatal("unmerged application promoted")
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE repository_pr_applications SET merged=TRUE WHERE id=$1`, v.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyRepositoryApplications(ctx, repo, "merge", []Application{tracked}); err != nil {
		t.Fatal(err)
	}
	promoted, err := s.ApplicationByID(ctx, id)
	if err != nil || promoted.RepositoryConfigurationID != repo.ID || promoted.Revision != repo.Revision || !promoted.AutoSyncPaused {
		t.Fatalf("handoff incorrect: %+v %v", promoted, err)
	}
	managed, err := s.ManagedResources(ctx, id)
	if err != nil || len(managed) != 1 {
		t.Fatal("promotion lost managed resource ownership", err)
	}
	v, err = s.RepositoryPRByID(ctx, v.ID)
	if err != nil || v.Phase != "promoted" {
		t.Fatal("promotion history missing", err)
	}
	if _, _, err = s.ReviewByPreviewApplication(ctx, id); err == nil {
		t.Fatal("promoted app still bound to PR")
	}
	testRepositoryPRNamespaceCreation(t, ctx, s)
}

func testRepositoryPRNamespaceCreation(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	repo := RepositoryConfiguration{ID: "namespace-creation-repo", WorkspaceID: "integration-workspace", SourceID: "integration-git", Revision: "namespace-main", Enabled: true}
	if err := s.CreateRepositoryConfiguration(ctx, repo); err != nil {
		t.Fatal(err)
	}
	policy := RepositoryPRSettings{Enabled: true, Provider: "gitlab", CredentialID: "integration-git-credential", Mode: "existing", Destinations: []PRDestination{{ClusterID: "integration-cluster", Namespace: "dev-created"}}, Profile: PreviewProfile{ConfirmShared: true}}
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, policy); err == nil {
		t.Fatal("exact destination without binding accepted without creation opt-in")
	}
	policy.AllowNamespaceCreation = true
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
		t.Fatal("new exact destination rejected with owner opt-in", err)
	}
	policy.Destinations = []PRDestination{{ClusterID: "integration-cluster", NamespacePattern: "dev-.*"}}
	policy.AllowNamespaceCreation = false
	savePolicy := func() {
		t.Helper()
		if err := s.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
			t.Fatal(err)
		}
		var err error
		repo, err = s.RepositoryConfigurationByID(ctx, repo.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	savePolicy()
	app := Application{WorkspaceID: repo.WorkspaceID, Name: "dev-created-app", SourceID: repo.SourceID, Revision: strings.Repeat("d", 40), ManifestPath: "dev", Renderer: "yaml", ClusterID: "integration-cluster", Namespaces: []NamespaceBinding{{Namespace: "dev-created"}}, CreateNamespaces: true, SyncPolicy: "manual", PollSeconds: 86400, RetryPolicy: DefaultRetryPolicy(), ConfigurationHash: "new-dev", ConfigurationPath: "dev/justcd.yaml"}
	register := func(candidate Application) (RepositoryPRApplication, error) {
		c := SourceControlConnection{ID: NewID(), Provider: "gitlab", APIURL: "https://gitlab.com/api/v4", Repository: "repo", StatusCredentialID: &policy.CredentialID, PreviewProfile: PreviewProfile{Enabled: true, DeploymentMode: "existing", ConfirmShared: true}}
		return s.UpsertRepositoryPRApplication(ctx, repo, RepositoryPRApplication{RepositoryID: repo.ID, Number: 99, DefinitionName: candidate.Name, Mode: "existing"}, candidate, c)
	}
	assertUnbound := func(namespace string) {
		t.Helper()
		if _, err := s.NamespaceBinding(ctx, repo.WorkspaceID, app.ClusterID, namespace); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("rejected discovery left namespace binding %q: %v", namespace, err)
		}
	}
	if _, err := register(app); err == nil {
		t.Fatal("branch enabled namespace creation without repository permission")
	}
	assertUnbound("dev-created")
	policy.AllowNamespaceCreation = true
	savePolicy()
	withoutRequest := app
	withoutRequest.CreateNamespaces = false
	if _, err := register(withoutRequest); err == nil {
		t.Fatal("namespace registered without application createNamespaces request")
	}
	assertUnbound("dev-created")
	outOfScope := app
	outOfScope.Namespaces = []NamespaceBinding{{Namespace: "production"}}
	if _, err := register(outOfScope); err == nil {
		t.Fatal("namespace creation escaped repository allowlist")
	}
	assertUnbound("production")
	// Missing defaults fail before the binding/app transaction can commit.
	credential, err := s.WorkspaceClusterCredential(ctx, repo.WorkspaceID, app.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkspaceClusterCredential(ctx, repo.WorkspaceID, app.ClusterID, nil); err != nil {
		t.Fatal(err)
	}
	_, missingDefault := register(app)
	if err := s.SetWorkspaceClusterCredential(ctx, repo.WorkspaceID, app.ClusterID, credential); err != nil {
		t.Fatal(err)
	}
	if missingDefault == nil || !strings.Contains(missingDefault.Error(), "default Kubernetes credential") {
		t.Fatalf("namespace registration without defaults: %v", missingDefault)
	}
	assertUnbound("dev-created")
	conflict := app
	conflict.Name = "git-managed"
	if _, err := register(conflict); err == nil {
		t.Fatal("duplicate application name accepted")
	}
	assertUnbound("dev-created")
	v, err := register(app)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.NamespaceBinding(ctx, repo.WorkspaceID, app.ClusterID, "dev-created")
	if err != nil || binding.CredentialID != nil {
		t.Fatalf("new binding did not inherit defaults: %+v %v", binding, err)
	}
	stored, err := s.ApplicationByID(ctx, *v.ApplicationID)
	if err != nil || !stored.CreateNamespaces || !stored.AutoSyncPaused {
		t.Fatalf("creation opt-in lost or reconciliation unpaused: %+v %v", stored, err)
	}
	again, err := register(app)
	if err != nil || *again.ApplicationID != stored.ID {
		t.Fatal("namespace/app discovery was not idempotent", err)
	}
	revoked := policy
	revoked.AllowNamespaceCreation = false
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, revoked); err == nil {
		t.Fatal("creation policy changed under an active PR")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE repository_pr_applications SET merged=TRUE WHERE id=$1`, v.ID); err != nil {
		t.Fatal(err)
	}
	app.Revision = repo.Revision
	app.ConfigurationHash = "merged-dev"
	if err := s.ApplyRepositoryApplications(ctx, repo, "merge-dev", []Application{app}); err != nil {
		t.Fatal("created dev namespace could not be promoted", err)
	}
	promoted, err := s.ApplicationByID(ctx, stored.ID)
	if err != nil || !promoted.CreateNamespaces || promoted.RepositoryConfigurationID != repo.ID || !promoted.AutoSyncPaused {
		t.Fatalf("namespace creation state lost on merge: %+v %v", promoted, err)
	}
	// Dev namespaces and bindings are shared infrastructure, retained on cleanup.
	if _, err := s.DeleteApplicationKeepingResources(ctx, stored.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NamespaceBinding(ctx, repo.WorkspaceID, app.ClusterID, "dev-created"); err != nil {
		t.Fatal("PR app cleanup deleted its dev namespace binding", err)
	}
}
