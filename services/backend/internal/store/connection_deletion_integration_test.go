//go:build integration

package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func testConnectionDeletionSafety(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	for _, target := range []struct{ kind, id string }{{"cluster", "integration-cluster"}, {"git_source", "integration-git"}, {"credential", "integration-workspace-credential"}, {"repository_configuration", "integration-repository"}} {
		if err := s.DeleteConnection(ctx, target.kind, target.id); !errors.Is(err, ErrConnectionInUse) {
			t.Fatalf("used %s deletion: %v", target.kind, err)
		}
	}
	if err := s.DeleteNamespaceBinding(ctx, "integration-workspace", "integration-cluster", "default"); !errors.Is(err, ErrConnectionInUse) {
		t.Fatalf("used namespace deletion: %v", err)
	}
	if err := s.CreateNamespaceBinding(ctx, "integration-workspace", "integration-cluster", "unused-namespace", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNamespaceBinding(ctx, "integration-workspace", "integration-cluster", "unused-namespace"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NamespaceBinding(ctx, "integration-workspace", "integration-cluster", "unused-namespace"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("binding remains: %v", err)
	}
	if err := s.DeleteNamespaceBinding(ctx, "another-workspace", "integration-cluster", "default"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted another workspace binding: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES('deletion-credential','integration-workspace','Unused','kubernetes-token',decode('00','hex'));
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('deletion-cluster','integration-workspace','Unused','https://unused.invalid');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('deletion-git','integration-workspace','Unused','https://unused.invalid/repository.git');
 INSERT INTO repository_configurations(id,workspace_id,source_id,revision) VALUES('deletion-repository','integration-workspace','deletion-git','main');
 INSERT INTO namespace_bindings(id,workspace_id,cluster_id,namespace) VALUES('deletion-binding','integration-workspace','deletion-cluster','unused');
 INSERT INTO workspaces(id,name) VALUES('deletion-recipient','Recipient');
 INSERT INTO workspace_cluster_shares(id,cluster_id,owner_workspace_id,target_workspace_id) VALUES('deletion-cluster-share','deletion-cluster','integration-workspace','deletion-recipient');
 INSERT INTO workspace_git_source_shares(id,git_source_id,owner_workspace_id,target_workspace_id) VALUES('deletion-git-share','deletion-git','integration-workspace','deletion-recipient');`); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct{ kind, id string }{{"cluster", "deletion-cluster"}, {"git_source", "deletion-git"}} {
		if err := s.DeleteConnection(ctx, target.kind, target.id); !errors.Is(err, ErrConnectionInUse) {
			t.Fatalf("active share deletion: %v", err)
		}
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE workspace_cluster_shares SET status='revoked' WHERE id='deletion-cluster-share'; UPDATE workspace_git_source_shares SET status='revoked' WHERE id='deletion-git-share'`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConnection(ctx, "repository_configuration", "deletion-repository"); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct{ kind, id string }{{"cluster", "deletion-cluster"}, {"git_source", "deletion-git"}, {"credential", "deletion-credential"}} {
		if err := s.DeleteConnection(ctx, target.kind, target.id); err != nil {
			t.Fatalf("unused %s deletion: %v", target.kind, err)
		}
		if err := s.DeleteConnection(ctx, target.kind, target.id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("repeat %s deletion: %v", target.kind, err)
		}
	}
}
