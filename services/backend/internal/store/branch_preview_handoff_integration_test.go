//go:build integration

package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func testPRBranchHandoff(t *testing.T, ctx context.Context, s *Store, parent Application) {
	t.Helper()
	previewID, err := s.StartBranchTest(ctx, parent, "feature/handoff", "isolated", "handoff-preview", "overlays/preview")
	if err != nil {
		t.Fatal(err)
	}
	connection := SourceControlConnection{ID: "handoff-connection", WorkspaceID: parent.WorkspaceID, ApplicationID: parent.ID, Provider: "github", APIURL: "https://api.github.com", Repository: "example/app", StatusTokenCipher: []byte{2}, PreviewProfile: PreviewProfile{Enabled: true, MaxActive: 1}}
	if err = s.SaveSourceControlConnection(ctx, connection); err != nil {
		t.Fatal(err)
	}
	review := PullRequestReview{ID: "handoff-review", ConnectionID: connection.ID, Number: 2, HeadSHA: strings.Repeat("a", 40), HeadBranch: "feature/handoff", SourceURL: "https://example.invalid/pr/2", EventAt: time.Now().UTC()}
	if _, err = s.RecordReviewEvent(ctx, connection.ID, "handoff-event", review); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET phase='adoption_available' WHERE id=$1`, review.ID); err != nil {
		t.Fatal(err)
	}
	if items, err := s.MatchingBranchPreviews(ctx, parent, review); err != nil || len(items) != 1 || items[0].ID != previewID {
		t.Fatalf("matching previews: %+v %v", items, err)
	}
	wrong := review
	wrong.HeadSHA = strings.Repeat("b", 40)
	if err = s.AdoptBranchPreview(ctx, wrong, parent, previewID, "refs/pull/2/head", 1); err == nil {
		t.Fatal("stale PR accepted")
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET lease_until=NOW()+INTERVAL '1 minute' WHERE id=$1`, review.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.AdoptBranchPreview(ctx, review, parent, previewID, "refs/pull/2/head", 1); err == nil {
		t.Fatal("handoff raced active worker")
	}
	if err = s.ReleaseReviewLease(ctx, review.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.AdoptBranchPreview(ctx, review, parent, previewID, "refs/pull/2/head", 1); err != nil {
		t.Fatal(err)
	}
	attached, err := s.ReviewByID(ctx, review.ID)
	if err != nil || attached.PreviewApplicationID == nil || *attached.PreviewApplicationID != previewID || !attached.AdoptedBranchPreview {
		t.Fatalf("handoff review: %+v %v", attached, err)
	}
	preview, err := s.ApplicationByID(ctx, previewID)
	if err != nil || preview.BranchTest != nil || preview.Revision != "refs/pull/2/head" || preview.Namespaces[0].Namespace != "handoff-preview" {
		t.Fatalf("handoff application: %+v %v", preview, err)
	}
	changed := connection
	changed.PreviewProfile.DeploymentMode = "existing"
	changed.PreviewProfile.ConfirmShared = true
	if err = s.SaveSourceControlConnection(ctx, changed); err != ErrActivePreviews {
		t.Fatalf("active destination change accepted: %v", err)
	}
	if err = s.AdoptBranchPreview(ctx, review, parent, previewID, "refs/pull/2/head", 1); err == nil {
		t.Fatal("duplicate handoff accepted")
	}
	if _, err = s.DeleteApplicationKeepingResources(ctx, previewID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.NamespaceBinding(ctx, parent.WorkspaceID, parent.ClusterID, "handoff-preview"); err != nil {
		t.Fatalf("borrowed namespace binding removed: %v", err)
	}
	if err = s.ReleasePreviewSlot(ctx, connection.ID, review.Number); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSourceControlConnection(ctx, changed); err != nil {
		t.Fatal(err)
	}
	shared := review
	shared.ID = "shared-review"
	shared.Number = 3
	shared.HeadBranch = "feature/shared"
	shared.EventAt = time.Now().UTC()
	if _, err = s.RecordReviewEvent(ctx, connection.ID, "shared-event", shared); err != nil {
		t.Fatal(err)
	}
	if err = s.AttachSharedPR(ctx, shared, parent, "refs/pull/3/head"); err != nil {
		t.Fatal(err)
	}
	pinned, err := s.ApplicationByID(ctx, parent.ID)
	if err != nil || pinned.BranchTest == nil || pinned.BranchTest.BaseRevision != parent.Revision || !pinned.AutoSyncPaused {
		t.Fatalf("shared destination pin: %+v %v", pinned, err)
	}
	if err = s.AttachSharedPR(ctx, shared, parent, "refs/pull/3/head"); err == nil {
		t.Fatal("duplicate shared deployment accepted")
	}
	if err = s.SaveSourceControlConnection(ctx, connection); err != ErrActivePreviews {
		t.Fatalf("shared destination changed while active: %v", err)
	}
	if err = s.ResumeBranchTest(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET preview_application_id=NULL,closed=TRUE WHERE id=$1`, shared.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteSourceControlConnection(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}
}
