//go:build integration

package store

import (
	"context"
	"strings"
	"testing"
)

func testBranchTestSafety(t *testing.T, ctx context.Context, s *Store, repository RepositoryConfiguration, definition Application) {
	t.Helper()
	apps, err := s.ListApplications(ctx, repository.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var app Application
	for _, item := range apps {
		if item.RepositoryConfigurationID == repository.ID {
			app = item
			break
		}
	}
	if app.ID == "" {
		t.Fatal("missing fixture application")
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES('branch-old-plan',$1,'main','old','[]','[]','integration-owner',NOW()+INTERVAL '15 minutes')`, app.ID); err != nil {
		t.Fatal(err)
	}
	id, err := s.StartBranchTest(ctx, app, "feature/database", "existing", "", "")
	if err != nil || id != app.ID {
		t.Fatalf("start branch test: %s %v", id, err)
	}
	active, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || active.BranchTest == nil || active.BranchTest.BaseRevision != app.Revision || active.Revision != "feature/database" || !active.AutoSyncPaused || active.Namespaces[0].Namespace != app.Namespaces[0].Namespace {
		t.Fatalf("branch override: %+v %v", active, err)
	}
	var status string
	if err = s.DB.QueryRowContext(ctx, `SELECT status FROM plans WHERE id='branch-old-plan'`).Scan(&status); err != nil || status != "stale" {
		t.Fatalf("old plan not invalidated: %s %v", status, err)
	}
	if _, err = s.StartBranchTest(ctx, active, "other", "existing", "", ""); err == nil {
		t.Fatal("nested branch test accepted")
	}
	if err = s.ResumeRollbackTracking(ctx, app.ID, ""); err == nil {
		t.Fatal("generic resume bypassed branch lifecycle")
	}
	changed := definition
	changed.ManifestPath = "latest-main"
	changed.ConfigurationHash = "main-updated-during-test"
	changed.ConfigurationPath = "renamed/application.yaml"
	if err = s.ApplyRepositoryApplications(ctx, repository, "new-main", []Application{changed}); err != nil {
		t.Fatal(err)
	}
	pinned, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || pinned.Revision != "feature/database" || pinned.ManifestPath != app.ManifestPath || pinned.ConfigurationPath != app.ConfigurationPath || pinned.ConfigurationCommit != app.ConfigurationCommit || !pinned.AutoSyncPaused {
		t.Fatalf("discovery overwrote branch test: %+v %v", pinned, err)
	}
	if err = s.ApplyRepositoryApplications(ctx, repository, "definition-removed", nil); err != nil {
		t.Fatal(err)
	}
	pinned, err = s.ApplicationByID(ctx, app.ID)
	if err != nil || pinned.ConfigurationMissing || pinned.BranchTest == nil {
		t.Fatalf("removed base definition interrupted branch test: %+v %v", pinned, err)
	}
	operation, err := s.AcquireResourceAction(ctx, app.ID, "integration-owner", 60*1e9)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResumeBranchTest(ctx, app.ID); err == nil || !strings.Contains(err.Error(), "syncing") {
		t.Fatalf("resume during operation: %v", err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE operations SET status='succeeded' WHERE id=$1`, operation); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `DELETE FROM operation_leases WHERE operation_id=$1`, operation); err != nil {
		t.Fatal(err)
	}
	if err = s.ResumeBranchTest(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || resumed.BranchTest != nil || resumed.Revision != repository.Revision || !resumed.AutoSyncPaused {
		t.Fatalf("unsafe resume: %+v %v", resumed, err)
	}
	if err = s.ApplyRepositoryApplications(ctx, repository, "new-main", []Application{changed}); err != nil {
		t.Fatal(err)
	}
	resumed, err = s.ApplicationByID(ctx, app.ID)
	if err != nil || resumed.ManifestPath != "latest-main" {
		t.Fatalf("resume did not adopt latest definition: %+v %v", resumed, err)
	}
	previewID, err := s.StartBranchTest(ctx, resumed, "feature/database", "isolated", "branch-preview", "overlays/preview")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ApplicationByID(ctx, previewID)
	if err != nil || preview.BranchTest == nil || preview.BranchTest.ParentApplicationID != app.ID || preview.RepositoryConfigurationID != "" || preview.SyncPolicy != "manual" || !preview.AutoSyncPaused || !preview.CreateNamespaces || preview.Namespaces[0].Namespace != "branch-preview" {
		t.Fatalf("isolated preview: %+v %v", preview, err)
	}
	if _, err = s.StartBranchTest(ctx, resumed, "feature/database", "isolated", "default", "overlays/preview"); err == nil {
		t.Fatal("preview accepted an existing workspace namespace")
	}
	if err = s.ResumeBranchTest(ctx, preview.ID); err == nil {
		t.Fatal("preview resumed as existing environment")
	}
	parent, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || parent.Revision != resumed.Revision || parent.BranchTest != nil {
		t.Fatalf("preview modified parent: %+v %v", parent, err)
	}
	connection := SourceControlConnection{ID: "branch-review-connection", WorkspaceID: app.WorkspaceID, ApplicationID: app.ID, Provider: "github", APIURL: "https://api.github.com", Repository: "example/app", StatusTokenCipher: []byte{2}}
	if err = s.SaveSourceControlConnection(ctx, connection); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO pull_request_reviews(id,connection_id,number,head_sha,source_url,event_at,preview_application_id) VALUES('branch-review',$1,1,$2,'https://example.invalid/review/1',NOW(),$3)`, connection.ID, strings.Repeat("a", 40), preview.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE applications SET branch_test=NULL WHERE id=$1`, preview.ID); err != nil {
		t.Fatal(err)
	}
	preview, err = s.ApplicationByID(ctx, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartBranchTest(ctx, preview, "other", "existing", "", ""); err == nil || !strings.Contains(err.Error(), "pull request") {
		t.Fatalf("branch testing bypassed PR preview lifecycle: %v", err)
	}
	if _, err = s.DeleteApplicationKeepingResources(ctx, preview.ID, true); err != nil {
		t.Fatal(err)
	}
	parent, err = s.ApplicationByID(ctx, app.ID)
	if err != nil || parent.Revision != resumed.Revision {
		t.Fatalf("preview cleanup modified parent: %+v %v", parent, err)
	}
	if err = s.DeleteSourceControlConnection(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	testPRBranchHandoff(t, ctx, s, parent)
}
