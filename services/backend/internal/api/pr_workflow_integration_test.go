//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationPRCredentialsAndCommentApprovals(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('pr-owner','owner@example.invalid'),('pr-viewer','viewer@example.invalid');
	INSERT INTO workspaces(id,name) VALUES('pr-workspace','PR test'),('pr-other','Other');
	INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('pr-workspace','pr-owner','owner'),('pr-workspace','pr-viewer','viewer');
	INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('pr-source','pr-workspace','Git','https://github.com/org/app.git');
	INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('pr-cluster','pr-workspace','Cluster','https://example.invalid');
	INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy) VALUES('pr-app','pr-workspace','App','pr-source','main','deploy','yaml','pr-cluster','[{"namespace":"dev"}]','manual')`)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	workspace := "pr-workspace"
	cipher, err := security.Encrypt(server.EncryptionKey, []byte(`{"token":"saved-api-token-123456789"}`), "credential:pr-credential")
	if err != nil {
		t.Fatal(err)
	}
	credential := store.Credential{ID: "pr-credential", WorkspaceID: &workspace, Name: "Existing Git token", Kind: "git-https", Cipher: cipher}
	if err = db.CreateCredential(ctx, credential); err != nil {
		t.Fatal(err)
	}
	connection := store.SourceControlConnection{ID: "pr-connection", WorkspaceID: workspace, ApplicationID: "pr-app", Provider: "github", Repository: "org/app", StatusCredentialID: &credential.ID, StatusTokenCipher: []byte{}, PreviewProfile: store.PreviewProfile{ApprovalActors: map[string]string{"10": "pr-viewer", "20": "pr-owner"}}}
	if token, err := server.sourceControlToken(ctx, connection); err != nil || string(token) != "saved-api-token-123456789" {
		t.Fatalf("saved credential not reused: %v", err)
	}
	connection.WorkspaceID = "pr-other"
	if _, err = server.sourceControlToken(ctx, connection); err == nil {
		t.Fatal("cross-workspace API credential accepted")
	}
	connection.WorkspaceID = workspace
	cipher, err = security.Encrypt(server.EncryptionKey, []byte(`{"token":"rotated-api-token-123456789"}`), "credential:pr-credential")
	if err != nil {
		t.Fatal(err)
	}
	credential.Cipher = cipher
	if err = db.UpdateCredential(ctx, credential); err != nil {
		t.Fatal(err)
	}
	if token, err := server.sourceControlToken(ctx, connection); err != nil || string(token) != "rotated-api-token-123456789" {
		t.Fatalf("credential rotation ignored: %v", err)
	}
	head := strings.Repeat("a", 40)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rotated-api-token-123456789" {
			t.Error("API did not reuse rotated token")
		}
		if strings.HasSuffix(r.URL.Path, "/comments") {
			now := time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "body": "/justcd approve pr-plan digest", "created_at": now, "user": map[string]int{"id": 10}},
				{"id": 2, "body": "/justcd approve pr-plan digest", "created_at": now, "user": map[string]int{"id": 30}},
				{"id": 3, "body": "/justcd approve pr-plan old-digest", "created_at": now, "user": map[string]int{"id": 20}},
				{"id": 4, "body": "/justcd approve pr-plan digest", "created_at": time.Now().Add(-time.Hour).Format(time.RFC3339), "user": map[string]int{"id": 20}},
				{"id": 5, "body": "/justcd approve pr-plan digest", "created_at": now, "user": map[string]int{"id": 20}},
			})
			return
		}
		// A commit arriving before apply must stop execution, even without a webhook.
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "open", "head": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]string{"full_name": "org/app"}}, "base": map[string]any{"repo": map[string]string{"full_name": "org/app"}}})
	}))
	defer provider.Close()
	connection.APIURL = provider.URL
	if err = db.SaveSourceControlConnection(ctx, connection); err != nil {
		t.Fatal(err)
	}
	if err = db.EnsureSystemActor(ctx); err != nil {
		t.Fatal(err)
	}
	record := store.PlanRecord{ID: "pr-plan", Plan: core.Plan{ApplicationID: "pr-app", Revision: head, Digest: "digest", RequiresApproval: true, RequiredApprovals: 1, ApproverRoles: []string{"owner"}}, CreatedBy: "pr-owner", Status: "current", ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err = db.SavePlan(ctx, record); err != nil {
		t.Fatal(err)
	}
	view := toPlanView(record)
	review := store.PullRequestReview{ID: "pr-review", Number: 1, HeadSHA: head, Phase: "approval_required"}
	review.Plan, _ = json.Marshal(view)
	for i := 0; i < 2; i++ {
		err = server.applyPRCommentApprovals(ctx, connection, &review, "rotated-api-token-123456789")
		if err == nil || !strings.Contains(err.Error(), "PR changed") {
			t.Fatalf("new commit was not blocked: %v", err)
		}
	}
	approvals, err := db.ListPlanApprovals(ctx, record.ID, record.Plan.Digest)
	if err != nil || len(approvals) != 1 || approvals[0].Approval.ActorID != "pr-owner" {
		t.Fatalf("unmapped/viewer/stale/duplicate comment approval accepted: %+v %v", approvals, err)
	}
	var operations int
	if err = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations`).Scan(&operations); err != nil || operations != 0 {
		t.Fatalf("stale PR queued operations: %d %v", operations, err)
	}
	testRepositoryPRAPIBoundaries(t, ctx, server)
}

func testRepositoryPRAPIBoundaries(t *testing.T, ctx context.Context, server *Server) {
	db := server.Store
	if err := db.CreateNamespaceBinding(ctx, "pr-workspace", "pr-cluster", "dev", nil); err != nil {
		t.Fatal(err)
	}
	repo := store.RepositoryConfiguration{ID: "api-bootstrap", WorkspaceID: "pr-workspace", SourceID: "pr-source", Revision: "main", Enabled: true}
	if err := db.CreateRepositoryConfiguration(ctx, repo); err != nil {
		t.Fatal(err)
	}
	policy := store.RepositoryPRSettings{Enabled: true, CredentialID: "pr-credential", Mode: "review-only", Destinations: []store.PRDestination{{ClusterID: "pr-cluster", Namespace: "dev"}}}
	if err := db.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
		t.Fatal(err)
	}
	repo, err := db.RepositoryConfigurationByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Repeat("c", 40)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "closed", "head": map[string]any{"sha": head, "ref": "new-app", "repo": map[string]string{"full_name": "org/app"}}, "base": map[string]any{"ref": "main", "repo": map[string]string{"full_name": "org/app"}}})
	}))
	defer provider.Close()
	app := store.Application{WorkspaceID: repo.WorkspaceID, Name: "review-only-bootstrap", SourceID: repo.SourceID, Revision: head, ManifestPath: "new-app", Renderer: "yaml", ClusterID: "pr-cluster", Namespaces: []store.NamespaceBinding{{Namespace: "dev"}}, SyncPolicy: "manual", PollSeconds: 86400, RetryPolicy: store.DefaultRetryPolicy(), ConfigurationHash: "one"}
	c := store.SourceControlConnection{ID: store.NewID(), Provider: "github", APIURL: provider.URL, Repository: "org/app", StatusCredentialID: &policy.CredentialID, PreviewProfile: store.PreviewProfile{}}
	v, err := db.UpsertRepositoryPRApplication(ctx, repo, store.RepositoryPRApplication{RepositoryID: repo.ID, Number: 43, DefinitionName: "new-app", Mode: "review-only"}, app, c)
	if err != nil {
		t.Fatal(err)
	}
	review, conn, err := db.ReviewByPreviewApplication(ctx, *v.ApplicationID)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := db.ApplicationByID(ctx, *v.ApplicationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.validatePRDeployment(ctx, conn, review, registered, store.PlanRecord{Plan: core.Plan{ApplicationID: registered.ID, Revision: head}}); err == nil || !strings.Contains(err.Error(), "policy does not permit") {
		t.Fatalf("review-only application allowed deployment: %v", err)
	}
	// Closure of an undeployed review-only app removes only JustCD configuration.
	for i := 0; i < 2; i++ {
		review, _, err = db.ReviewByPreviewApplication(ctx, *v.ApplicationID)
		if err != nil {
			t.Fatal(err)
		}
		if err = server.processReview(ctx, review); err != nil {
			t.Fatal(err)
		}
	}
	history, err := db.RepositoryPRByID(ctx, v.ID)
	if err != nil || history.Phase != "removed" || history.ApplicationID != nil || history.ConnectionID != nil {
		t.Fatalf("cleanup history lost: %+v %v", history, err)
	}
	if _, err = db.NamespaceBinding(ctx, repo.WorkspaceID, "pr-cluster", "dev"); err != nil {
		t.Fatal("review-only cleanup removed existing namespace binding", err)
	}
}
