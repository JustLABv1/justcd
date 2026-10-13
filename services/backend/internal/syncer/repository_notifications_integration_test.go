//go:build integration

package syncer

import (
	"context"
	"database/sql"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/store"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationRepositoryNotifications(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "justcd_notifications_repo_" + store.NewID()[:8]
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	parsed, _ := url.Parse(dsn)
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	db, err := store.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	owner, workspace, cluster, source := store.NewID(), store.NewID(), store.NewID(), store.NewID()
	_, err = db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES($1,'notify-owner@example.invalid')`, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateWorkspace(ctx, store.Workspace{ID: workspace, Name: "Notifications"}, owner); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCluster(ctx, store.Cluster{ID: cluster, Name: "production", APIServer: "https://example.invalid", CAData: []byte{}}); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateNamespaceBinding(ctx, workspace, cluster, "prod", nil); err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.ExecContext(ctx, `INSERT INTO notification_destinations(id,workspace_id,name,provider,events,minimum_severity,config_cipher) VALUES('prod-telegram',$1,'Production Telegram','telegram','["unhealthy"]','error','cipher')`, workspace)
	if err != nil {
		t.Fatal(err)
	}
	repo := newIntegrationGitRepo(t, "prod")
	base := `apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: prod
spec:
  source:
    renderer: yaml
    path: .
  destination:
    cluster: production
    namespace: prod
`
	repo.write(t, "yaml/justcd.yaml", base)
	repo.commit(t, "notification fixture")
	if err = db.CreateGitSource(ctx, store.GitSource{ID: source, WorkspaceID: workspace, Name: "Source", RepositoryURL: repo.url}); err != nil {
		t.Fatal(err)
	}
	repository := store.RepositoryConfiguration{ID: store.NewID(), WorkspaceID: workspace, SourceID: source, Revision: "main", Enabled: true}
	if err = db.CreateRepositoryConfiguration(ctx, repository); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: db}
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	apps, err := db.ListApplications(ctx, workspace)
	if err != nil || len(apps) != 1 {
		t.Fatal("discovery", err)
	}
	app := apps[0]
	assertPolicy := func(mode string, managed bool) {
		t.Helper()
		p, err := db.ApplicationNotificationPolicy(ctx, app.ID)
		if err != nil || p.Mode != mode || p.ManagedByGit != managed {
			t.Fatalf("policy: %+v %v", p, err)
		}
	}
	assertPolicy("inherit", false)
	if err = db.SaveApplicationNotificationPolicy(ctx, app.ID, workspace, owner, store.ApplicationNotificationPolicy{Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	assertPolicy("disabled", false) // omitted YAML retains UI policy
	custom := `  notifications:
    mode: custom
    destinations: ["Production Telegram"]
    events: [sync_failed, unhealthy]
    minimumSeverity: warning
`
	repo.write(t, "yaml/justcd.yaml", base+custom)
	repo.commit(t, "Git owns notifications")
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	assertPolicy("custom", true)
	p, _ := db.ApplicationNotificationPolicy(ctx, app.ID)
	if len(p.DestinationIDs) != 1 || p.DestinationIDs[0] != "prod-telegram" || p.MinimumSeverity != "warning" {
		t.Fatal("name resolution", p)
	}
	if err = db.SaveApplicationNotificationPolicy(ctx, app.ID, workspace, owner, store.ApplicationNotificationPolicy{Mode: "disabled"}); !errors.Is(err, store.ErrNotificationPolicyManagedByGit) {
		t.Fatal("manual overwrite of Git policy accepted", err)
	}
	count := func() int {
		var n int
		_ = db.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE action='notification.application_policy_saved' AND resource_id=$1`, app.ID).Scan(&n)
		return n
	}
	before := count()
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	if count() != before {
		t.Fatal("unchanged reconciliation repeats policy audit")
	}
	current, _ := db.ApplicationByID(ctx, app.ID)
	hash := current.ConfigurationHash
	// A second invalid definition cannot partially update the first application.
	repo.write(t, "yaml/justcd.yaml", base+"  notifications:\n    mode: disabled\n")
	bad := strings.Replace(base, "name: prod", "name: bad", 1) + strings.Replace(custom, "Production Telegram", "Unavailable destination", 1)
	repo.write(t, "kustomize/justcd.yaml", bad)
	repo.commit(t, "invalid atomic snapshot")
	if err = svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("missing destination accepted")
	}
	assertPolicy("custom", true)
	current, _ = db.ApplicationByID(ctx, app.ID)
	if current.ConfigurationHash != hash {
		t.Fatal("invalid snapshot partially changed metadata")
	}
	repo.write(t, "kustomize/justcd.yaml", "")
	_, err = db.DB.ExecContext(ctx, `INSERT INTO workspaces(id,name) VALUES('foreign-notifications','Other');INSERT INTO notification_destinations(id,workspace_id,name,provider,events,minimum_severity,config_cipher) VALUES('foreign-destination','foreign-notifications','Other','slack','["sync_failed"]','error','cipher')`)
	if err != nil {
		t.Fatal(err)
	}
	repo.write(t, "yaml/justcd.yaml", base+strings.Replace(custom, "Production Telegram", "foreign-destination", 1))
	repo.commit(t, "foreign destination")
	if err = svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("cross-workspace destination accepted")
	}
	assertPolicy("custom", true)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO notification_destinations(id,workspace_id,name,provider,events,minimum_severity,config_cipher) VALUES('duplicate',$1,'Production Telegram','telegram','["unhealthy"]','error','cipher')`, workspace)
	if err != nil {
		t.Fatal(err)
	}
	repo.write(t, "yaml/justcd.yaml", base+custom)
	repo.commit(t, "ambiguous destination name")
	if err = svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("ambiguous name accepted")
	}
	assertPolicy("custom", true)
	repo.write(t, "yaml/justcd.yaml", base+strings.Replace(custom, "Production Telegram", "prod-telegram", 1))
	repo.commit(t, "explicit destination ID")
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	assertPolicy("custom", true)
	if _, err = db.DB.ExecContext(ctx, `INSERT INTO notification_events(id,workspace_id,application_id,event_type,severity) VALUES('repo-notification',$1,$2,'sync_failed','warning')`, workspace, app.ID); err != nil {
		t.Fatal(err)
	}
	var queued int
	_ = db.DB.QueryRowContext(ctx, `SELECT count(*) FROM notification_deliveries WHERE event_id='repo-notification' AND destination_id='prod-telegram' AND status='queued'`).Scan(&queued)
	if queued != 1 {
		t.Fatal("Git routing not effective")
	}
	repo.write(t, "yaml/justcd.yaml", base+"  notifications:\n    mode: disabled\n")
	repo.commit(t, "disable via Git")
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	assertPolicy("disabled", true)
	var status string
	_ = db.DB.QueryRowContext(ctx, `SELECT status FROM notification_deliveries WHERE event_id='repo-notification'`).Scan(&status)
	if status != "disabled" {
		t.Fatal("Git disable left queued delivery active")
	}
	// Rollback and branch tests freeze policy updates until explicitly resumed.
	for _, column := range []string{"rollback_resume_state", "branch_test"} {
		if _, err = db.DB.ExecContext(ctx, `UPDATE applications SET `+column+`='{}' WHERE id=$1`, app.ID); err != nil {
			t.Fatal(err)
		}
		repo.write(t, "yaml/justcd.yaml", base+custom)
		repo.commit(t, "policy change while frozen "+column) // use unambiguous ID while a frozen app is resolved
		repo.write(t, "yaml/justcd.yaml", base+strings.Replace(custom, "Production Telegram", "prod-telegram", 1))
		repo.commit(t, "unambiguous frozen policy")
		if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
			t.Fatal(err)
		}
		assertPolicy("disabled", true)
		if _, err = db.DB.ExecContext(ctx, `UPDATE applications SET `+column+`=NULL WHERE id=$1`, app.ID); err != nil {
			t.Fatal(err)
		}
	}
	repo.write(t, "yaml/justcd.yaml", base+"  notifications:\n    mode: inherit\n")
	repo.commit(t, "inherit via Git")
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	assertPolicy("inherit", true)
	repo.write(t, "yaml/justcd.yaml", base)
	repo.commit(t, "remove Git policy")
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	assertPolicy("inherit", false)
	if err = db.SaveApplicationNotificationPolicy(ctx, app.ID, workspace, owner, store.ApplicationNotificationPolicy{Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	assertPolicy("disabled", false)
}
