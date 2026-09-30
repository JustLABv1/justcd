//go:build integration

package store

import (
	"context"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/core"
	"strings"
	"testing"
)

func testRepositoryConfigurationSafety(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	repository := RepositoryConfiguration{ID: "integration-repository", WorkspaceID: "integration-workspace", SourceID: "integration-git", Revision: "main", Enabled: true}
	if err := s.CreateRepositoryConfiguration(ctx, repository); err != nil {
		t.Fatal(err)
	}
	binding, err := s.NamespaceBinding(ctx, repository.WorkspaceID, "integration-cluster", "default")
	if err != nil {
		t.Fatal(err)
	}
	app := Application{WorkspaceID: repository.WorkspaceID, Name: "git-managed", SourceID: repository.SourceID, Revision: repository.Revision, ManifestPath: "manifests", Renderer: "yaml", ClusterID: "integration-cluster", Namespaces: []NamespaceBinding{binding}, SyncPolicy: "manual", PollSeconds: 300, RetryPolicy: DefaultRetryPolicy(), ConfigurationPath: "manifests/justcd.yaml", ConfigurationHash: "first"}
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-1", []Application{app}); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListApplications(ctx, repository.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var stored Application
	for _, candidate := range all {
		if candidate.Name == app.Name {
			stored = candidate
		}
	}
	if stored.ID == "" || stored.RepositoryConfigurationID != repository.ID || stored.ConfigurationCommit != "commit-1" || stored.ConfigurationPath != app.ConfigurationPath {
		t.Fatalf("missing Git provenance: %+v", stored)
	}
	originalID := stored.ID
	for _, check := range []struct {
		id   string
		want bool
	}{{originalID, true}, {"deleted-application", false}} {
		exists, err := s.ApplicationExists(ctx, check.id)
		if err != nil || exists != check.want {
			t.Fatalf("owner existence %s: %v %v", check.id, exists, err)
		}
	}

	// Git exclusions reconcile atomically while manual rules survive.
	manual := core.IgnoreRule{ID: "manual-preserved", Identity: core.Identity{ClusterID: app.ClusterID, APIVersion: "v1", Kind: "Secret", Namespace: "default", Name: "manual"}, Reason: "Manual ownership"}
	if err := s.CreateIgnoreRule(ctx, originalID, "integration-owner", manual); err != nil {
		t.Fatal(err)
	}
	app.RepositoryIgnoreRules = []core.IgnoreRule{{Identity: core.Identity{ClusterID: app.ClusterID, APIVersion: "v1", Kind: "Secret", Namespace: "default", Name: "vault"}, Reason: "Vault managed"}}
	app.RepositoryIgnoreSelectors = []core.IgnoreSelector{{APIVersion: "secrets.hashicorp.com/v1beta1", Kind: "VaultStaticSecret", Reason: "Vault managed"}}
	app.ConfigurationHash = "with-ignores"
	if err := s.ApplyRepositoryApplications(ctx, repository, "ignore-commit", []Application{app}); err != nil {
		t.Fatal(err)
	}
	rules, err := s.IgnoreRules(ctx, originalID)
	if err != nil || len(rules) != 2 {
		t.Fatalf("rules: %+v %v", rules, err)
	}
	var gitRuleID string
	for _, rule := range rules {
		if rule.ManagedByGit {
			gitRuleID = rule.ID
		}
	}
	if gitRuleID == "" {
		t.Fatal("Git provenance missing")
	}
	if _, err := s.DeleteIgnoreRule(ctx, originalID, gitRuleID); !errors.Is(err, ErrGitManagedIgnore) {
		t.Fatalf("Git rule deletion allowed: %v", err)
	}
	selectors, err := s.IgnoreSelectors(ctx, originalID)
	if err != nil || len(selectors) != 1 || !selectors[0].ManagedByGit {
		t.Fatalf("selectors: %+v %v", selectors, err)
	}
	if err := s.ChangeIgnoreSelector(ctx, originalID, "integration-owner", core.IgnoreSelector{}, selectors[0].ID); !errors.Is(err, ErrGitManagedIgnore) {
		t.Fatalf("Git selector deletion allowed: %v", err)
	}
	// A manual-rule collision must roll back deletion of previous Git rules.
	app.RepositoryIgnoreRules = []core.IgnoreRule{manual}
	app.ConfigurationHash = "conflicting-ignores"
	if err := s.ApplyRepositoryApplications(ctx, repository, "conflict", []Application{app}); err == nil {
		t.Fatal("manual rule adopted")
	}
	rules, err = s.IgnoreRules(ctx, originalID)
	if err != nil || len(rules) != 2 {
		t.Fatalf("failed snapshot changed rules: %+v %v", rules, err)
	}
	app.RepositoryIgnoreRules = nil
	app.RepositoryIgnoreSelectors = nil
	app.ConfigurationHash = "first"
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-1", []Application{app}); err != nil {
		t.Fatal(err)
	}
	rules, err = s.IgnoreRules(ctx, originalID)
	if err != nil || len(rules) != 1 || rules[0].ManagedByGit {
		t.Fatalf("manual exclusion lost: %+v %v", rules, err)
	}
	selectors, err = s.IgnoreSelectors(ctx, originalID)
	if err != nil || len(selectors) != 0 {
		t.Fatalf("removed selectors retained: %+v %v", selectors, err)
	}
	if _, err := s.DeleteIgnoreRule(ctx, originalID, manual.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.PauseApplication(ctx, originalID); err != nil {
		t.Fatal(err)
	}
	// Unchanged definitions must preserve operational state and application identity.
	if _, err := s.DB.ExecContext(ctx, `UPDATE applications SET health='synced',retry_attempt_count=3 WHERE id=$1`, originalID); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-2", []Application{app}); err != nil {
		t.Fatal(err)
	}
	stored, err = s.ApplicationByID(ctx, originalID)
	if err != nil || !stored.AutoSyncPaused || stored.Health != "synced" || stored.RetryAttemptCount != 3 {
		t.Fatalf("unchanged definition reset state: %+v %v", stored, err)
	}
	// A conflict later in the snapshot rolls back earlier updates.
	app.ManifestPath = "changed"
	app.ConfigurationHash = "second"
	conflict := app
	conflict.Name = "Due application"
	if err := s.ApplyRepositoryApplications(ctx, repository, "bad-commit", []Application{app, conflict}); err == nil {
		t.Fatal("manual application adopted")
	}
	stored, err = s.ApplicationByID(ctx, originalID)
	if err != nil || stored.ManifestPath != "manifests" || stored.ConfigurationCommit != "commit-2" {
		t.Fatalf("snapshot partially committed: %+v %v", stored, err)
	}
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-3", []Application{app}); err != nil {
		t.Fatal(err)
	}
	stored, err = s.ApplicationByID(ctx, originalID)
	if err != nil || stored.ManifestPath != "changed" || !stored.AutoSyncPaused {
		t.Fatalf("valid update lost identity or pause: %+v %v", stored, err)
	}
	// Managed targets stay protected; JSONB whitespace must not block safe source edits.
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO managed_resources(application_id,cluster_id,api_version,kind,namespace,name,uid,manifest) VALUES($1,'integration-cluster','v1','ConfigMap','default','fixture','fixture-uid','{}'::jsonb)`, originalID); err != nil {
		t.Fatal(err)
	}
	app.ConfigurationHash = "third"
	app.ManifestPath = "safe-source-change"
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-4", []Application{app}); err != nil {
		t.Fatalf("safe source update with managed resources: %v", err)
	}
	invalidTarget := app
	invalidTarget.ConfigurationHash = "invalid-target"
	invalidTarget.ClusterID = "integration-owned-cluster"
	if err := s.ApplyRepositoryApplications(ctx, repository, "bad-target", []Application{invalidTarget}); err == nil || !strings.Contains(err.Error(), "cannot change cluster") {
		t.Fatalf("managed target changed: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES('repository-before-removal',$1,'main','before','[]','[]','integration-owner',NOW()+INTERVAL '15 minutes')`, originalID); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-5", nil); err != nil {
		t.Fatal(err)
	}
	stored, err = s.ApplicationByID(ctx, originalID)
	if err != nil || !stored.ConfigurationMissing || stored.Decommissioning {
		t.Fatalf("missing definition deleted application: %+v %v", stored, err)
	}
	var status string
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM plans WHERE id='repository-before-removal'`).Scan(&status); err != nil || status != "stale" {
		t.Fatalf("removal did not invalidate old plan: %s %v", status, err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at,decommission) VALUES('repository-manual-decommission',$1,'main','manual','[]','[]','integration-owner',NOW()+INTERVAL '15 minutes',TRUE)`, originalID); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-5", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM plans WHERE id='repository-manual-decommission'`).Scan(&status); err != nil || status != "current" {
		t.Fatalf("repeated discovery invalidated reviewed manual deletion: %s %v", status, err)
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_resources WHERE application_id=$1`, originalID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("resources removed: %d %v", count, err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=FALSE WHERE id=$1`, originalID); err != nil {
		t.Fatal(err)
	}
	due, err := s.DueApplications(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range due {
		if candidate.ID == originalID {
			t.Fatal("missing definition remains due for auto sync")
		}
	}
	if err := s.ApplyRepositoryApplications(ctx, repository, "commit-6", []Application{app}); err != nil {
		t.Fatal(err)
	}
	stored, err = s.ApplicationByID(ctx, originalID)
	if err != nil || stored.ConfigurationMissing {
		t.Fatalf("restored definition not resumed: %+v %v", stored, err)
	}
	if err := s.SetRepositoryConfigurationEnabled(ctx, repository.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRepositoryApplications(ctx, repository, "disabled", nil); err == nil {
		t.Fatal("paused discovery applied a snapshot")
	}
	if err := s.RecordRepositoryConfigurationError(ctx, repository.ID, "invalid configuration"); err != nil {
		t.Fatal(err)
	}
	recorded, err := s.RepositoryConfigurationByID(ctx, repository.ID)
	if err != nil || recorded.LastCommit != "commit-6" || recorded.LastError == "" {
		t.Fatalf("failed discovery lost last valid commit: %+v %v", recorded, err)
	}
}
