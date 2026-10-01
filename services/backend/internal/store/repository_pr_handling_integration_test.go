//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
)

func testRepositoryPRHandling(t *testing.T, ctx context.Context, s *Store) {
	repository := RepositoryConfiguration{ID: "pr-config-repository", WorkspaceID: "integration-workspace", SourceID: "integration-git", Revision: "pr-config", Enabled: true}
	if err := s.CreateRepositoryConfiguration(ctx, repository); err != nil {
		t.Fatal(err)
	}
	binding, err := s.NamespaceBinding(ctx, repository.WorkspaceID, "integration-cluster", "default")
	if err != nil {
		t.Fatal(err)
	}
	app := Application{WorkspaceID: repository.WorkspaceID, Name: "declarative-pr", SourceID: repository.SourceID, Revision: "main", ManifestPath: "manifests", Renderer: "yaml", ClusterID: "integration-cluster", Namespaces: []NamespaceBinding{binding}, SyncPolicy: "manual", PollSeconds: 300, RetryPolicy: DefaultRetryPolicy(), ConfigurationPath: "justcd.yaml", ConfigurationHash: "one", RepositoryPullRequests: &RepositoryPRConfig{Enabled: true, Provider: "gitlab", CredentialID: "integration-git-credential"}}
	if err := s.ApplyRepositoryApplications(ctx, repository, "one", []Application{app}); err != nil {
		t.Fatal(err)
	}
	apps, err := s.ListApplications(ctx, repository.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range apps {
		if a.Name == app.Name {
			app.ID = a.ID
		}
	}
	c, err := s.SourceControlConnectionByApplication(ctx, app.ID)
	if err != nil || !c.Enabled || !c.ManagedByGit || c.StatusCredentialID == nil || *c.StatusCredentialID != "integration-git-credential" || c.Provider != "gitlab" || c.Repository != "repo" {
		t.Fatalf("PR connection missing: %+v %v", c, err)
	}
	if err := s.SetSourceControlConnectionEnabled(ctx, app.ID, false); !errors.Is(err, ErrGitManagedPR) {
		t.Fatalf("manual disable accepted: %v", err)
	}
	if err := s.DeleteSourceControlConnection(ctx, app.ID); !errors.Is(err, ErrGitManagedPR) {
		t.Fatalf("manual delete accepted: %v", err)
	}
	if err := s.SaveSourceControlConnection(ctx, c); !errors.Is(err, ErrGitManagedPR) {
		t.Fatalf("manual configuration accepted: %v", err)
	}
	app.RepositoryPullRequests.CredentialID = "integration-credential"
	app.ConfigurationHash = "invalid"
	if err := s.ApplyRepositoryApplications(ctx, repository, "invalid", []Application{app}); err == nil {
		t.Fatal("non-HTTPS credential accepted")
	}
	unchanged, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || unchanged.ConfigurationHash != "one" {
		t.Fatal("invalid PR configuration partially applied")
	}
	app.RepositoryPullRequests.CredentialID = "integration-git-credential"
	app.ConfigurationHash = "two"
	app.RepositoryPullRequests.Enabled = false
	if err := s.ApplyRepositoryApplications(ctx, repository, "two", []Application{app}); err != nil {
		t.Fatal(err)
	}
	c, err = s.SourceControlConnectionByApplication(ctx, app.ID)
	if err != nil || c.Enabled {
		t.Fatal("disabled config not reconciled")
	}
	app.RepositoryPullRequests.Enabled = true
	if err := s.ApplyRepositoryApplications(ctx, repository, "three", []Application{app}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO pull_request_reviews(id,connection_id,number,head_sha,source_url,event_at,preview_application_id) VALUES('managed-pr-active',$1,99,repeat('a',40),'https://example.invalid/pr',NOW(),$2)`, c.ID, app.ID); err != nil {
		t.Fatal(err)
	}
	app.RepositoryPullRequests = nil
	if err := s.ApplyRepositoryApplications(ctx, repository, "remove", []Application{app}); !errors.Is(err, ErrActivePreviews) {
		t.Fatalf("active preview guard missing: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM pull_request_reviews WHERE id='managed-pr-active'`); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRepositoryApplications(ctx, repository, "remove", []Application{app}); err != nil {
		t.Fatal(err)
	}
	c, err = s.SourceControlConnectionByApplication(ctx, app.ID)
	if err != nil || c.Enabled || c.ManagedByGit {
		t.Fatal("removed config did not release control safely")
	}
}
