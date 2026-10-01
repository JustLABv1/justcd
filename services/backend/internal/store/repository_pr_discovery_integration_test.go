//go:build integration

package store

import (
	"context"
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
	repo, err := s.RepositoryConfigurationByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
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
}
