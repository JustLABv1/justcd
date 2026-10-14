//go:build integration

package store

import (
	"context"
	"testing"
)

// Runs after an upgrade with an application seeded under the old constraint.
func testClusterScopedApplicationNames(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	original, err := s.ApplicationByID(ctx, "integration-app")
	if err != nil {
		t.Fatal(err)
	}
	const secondCluster = "name-scope-cluster"
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO clusters(id,name,api_server) VALUES($1,'Name scope cluster','https://example.invalid')`, secondCluster); err != nil {
		t.Fatal(err)
	}
	credential, err := s.WorkspaceClusterCredential(ctx, original.WorkspaceID, original.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkspaceClusterCredential(ctx, original.WorkspaceID, secondCluster, credential); err != nil {
		t.Fatal(err)
	}
	originalBinding, err := s.NamespaceBinding(ctx, original.WorkspaceID, original.ClusterID, "default")
	if err != nil {
		t.Fatal(err)
	}
	original.Namespaces = []NamespaceBinding{originalBinding}
	if err := s.CreateNamespaceBinding(ctx, original.WorkspaceID, secondCluster, "default", nil); err != nil {
		t.Fatal(err)
	}
	binding, err := s.NamespaceBinding(ctx, original.WorkspaceID, secondCluster, "default")
	if err != nil {
		t.Fatal(err)
	}
	sibling := original
	sibling.ID = NewID()
	sibling.ClusterID = secondCluster
	sibling.Namespaces = []NamespaceBinding{binding}
	if err := s.CreateApplication(ctx, sibling); err != nil {
		t.Fatalf("same name on another cluster: %v", err)
	}
	duplicate := sibling
	duplicate.ID = NewID()
	if err := s.CreateApplication(ctx, duplicate); err == nil {
		t.Fatal("same cluster duplicate accepted")
	}
	collision := sibling
	collision.ClusterID = original.ClusterID
	collision.Namespaces = original.Namespaces
	if err := s.UpdateApplication(ctx, collision); err == nil {
		t.Fatal("cluster edit accepted conflicting name")
	}
	renamed := sibling
	renamed.ID = NewID()
	renamed.Name = "name-scope-other"
	if err := s.CreateApplication(ctx, renamed); err != nil {
		t.Fatal(err)
	}
	renamed.Name = sibling.Name
	if err := s.UpdateApplication(ctx, renamed); err == nil {
		t.Fatal("rename accepted conflicting name")
	}
	survivor, err := s.ApplicationByID(ctx, original.ID)
	if err != nil || survivor.Name != original.Name || survivor.ClusterID != original.ClusterID || survivor.Revision != original.Revision {
		t.Fatalf("existing application changed: %+v %v", survivor, err)
	}

	repo := RepositoryConfiguration{ID: NewID(), WorkspaceID: original.WorkspaceID, SourceID: original.SourceID, Revision: "name-scope", Enabled: true}
	if err := s.CreateRepositoryConfiguration(ctx, repo); err != nil {
		t.Fatal(err)
	}
	a := original
	a.Name = "name-scope-git"
	a.ConfigurationPath = "a/justcd.yaml"
	a.ConfigurationHash = "a-first"
	b := sibling
	b.Name = a.Name
	b.ConfigurationPath = "b/justcd.yaml"
	b.ConfigurationHash = "b-first"
	if err := s.ApplyRepositoryApplications(ctx, repo, "first", []Application{a, b}); err != nil {
		t.Fatalf("same repository, different clusters: %v", err)
	}
	load := func(clusterID string) Application {
		t.Helper()
		app, err := scanApplication(s.DB.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE repository_configuration_id=$1 AND cluster_id=$2 AND name=$3`, repo.ID, clusterID, a.Name))
		if err != nil {
			t.Fatal(err)
		}
		return app
	}
	storedA, storedB := load(a.ClusterID), load(b.ClusterID)
	if storedA.ID == storedB.ID {
		t.Fatal("cluster identities merged")
	}
	if err := s.PauseApplication(ctx, storedA.ID); err != nil {
		t.Fatal(err)
	}
	a.ConfigurationHash = "a-updated"
	a.ConfigurationPath = "moved/justcd.yaml"
	a.ManifestPath = "updated"
	if err := s.ApplyRepositoryApplications(ctx, repo, "second", []Application{b, a}); err != nil {
		t.Fatal(err)
	}
	updatedA, unchangedB := load(a.ClusterID), load(b.ClusterID)
	if updatedA.ID != storedA.ID || !updatedA.AutoSyncPaused || updatedA.ManifestPath != "updated" || unchangedB.ID != storedB.ID || unchangedB.ConfigurationHash != "b-first" {
		t.Fatalf("reconciliation crossed clusters: %+v %+v", updatedA, unchangedB)
	}
	if err := s.ApplyRepositoryApplications(ctx, repo, "duplicate", []Application{a, a}); err == nil {
		t.Fatal("duplicate resolved destination accepted")
	}
	conflict := a
	conflict.Name = original.Name
	if err := s.ApplyRepositoryApplications(ctx, repo, "conflict", []Application{a, b, conflict}); err == nil {
		t.Fatal("manual app adopted on same cluster")
	}
	if err := s.ApplyRepositoryApplications(ctx, repo, "removed", []Application{b}); err != nil {
		t.Fatal(err)
	}
	if !load(a.ClusterID).ConfigurationMissing || load(b.ClusterID).ConfigurationMissing {
		t.Fatal("removal crossed clusters")
	}
	if err := s.ApplyRepositoryApplications(ctx, repo, "restored", []Application{a, b}); err != nil {
		t.Fatal(err)
	}
	if load(a.ClusterID).ID != storedA.ID || load(a.ClusterID).ConfigurationMissing {
		t.Fatal("restore lost identity")
	}
	testClusterScopedRepositoryPRNames(t, ctx, s, a, b)
	// An app with the same name on the other cluster must not block an import.
	imported := a
	imported.Name = "name-scope-other"
	if err := s.ApplyRepositoryApplications(ctx, repo, "cross-manual", []Application{a, b, imported}); err != nil {
		t.Fatalf("manual app on other cluster blocked import: %v", err)
	}
}

func testClusterScopedRepositoryPRNames(t *testing.T, ctx context.Context, s *Store, a, b Application) {
	t.Helper()
	repo := RepositoryConfiguration{ID: NewID(), WorkspaceID: a.WorkspaceID, SourceID: a.SourceID, Revision: "name-scope-pr", Enabled: true}
	if err := s.CreateRepositoryConfiguration(ctx, repo); err != nil {
		t.Fatal(err)
	}
	policy := RepositoryPRSettings{Enabled: true, Provider: "gitlab", CredentialID: "integration-git-credential", Mode: "existing", Destinations: []PRDestination{{ClusterID: a.ClusterID, Namespace: "default"}, {ClusterID: b.ClusterID, Namespace: "default"}}, Profile: PreviewProfile{ConfirmShared: true, MaxActive: 10}}
	if err := s.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
		t.Fatal(err)
	}
	repo, err := s.RepositoryConfigurationByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	register := func(app Application) RepositoryPRApplication {
		t.Helper()
		c := SourceControlConnection{ID: NewID(), Provider: "gitlab", APIURL: "https://gitlab.com/api/v4", Repository: "repo", StatusCredentialID: &policy.CredentialID, PreviewProfile: PreviewProfile{Enabled: true, DeploymentMode: "existing", ConfirmShared: true}}
		v, err := s.UpsertRepositoryPRApplication(ctx, repo, RepositoryPRApplication{RepositoryID: repo.ID, Number: 123, DefinitionName: app.Name, ConfigurationPath: app.ConfigurationPath, Mode: "existing"}, app, c)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	a.Name, b.Name = "name-scope-pr", "name-scope-pr"
	first, second := register(a), register(b)
	if first.ID == second.ID || *first.ApplicationID == *second.ApplicationID || first.ClusterID != a.ClusterID || second.ClusterID != b.ClusterID {
		t.Fatalf("PR cluster identities merged: %+v %+v", first, second)
	}
	if register(a).ID != first.ID || register(b).ID != second.ID {
		t.Fatal("PR re-registration lost identity")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE repository_pr_applications SET merged=TRUE WHERE repository_id=$1`, repo.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRepositoryApplications(ctx, repo, "merged", []Application{a, b}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []RepositoryPRApplication{first, second} {
		promoted, err := s.ApplicationByID(ctx, *v.ApplicationID)
		if err != nil || promoted.RepositoryConfigurationID != repo.ID || promoted.ClusterID != v.ClusterID {
			t.Fatalf("promotion lost cluster or ownership: %+v %v", promoted, err)
		}
		record, err := s.RepositoryPRByID(ctx, v.ID)
		if err != nil || record.Phase != "promoted" {
			t.Fatalf("PR not promoted: %+v %v", record, err)
		}
	}
}
