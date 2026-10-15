//go:build integration

package syncer

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationRuntimeTimeoutPublishesUnknownHealth(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	observeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	local := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel() // Deterministically expire observation while Kubernetes is read.
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer local.Close()
	key := []byte(strings.Repeat("k", 32))
	workspace, credentialID := "workspace", "runtime-kube"
	cipher, err := security.Encrypt(key, []byte(`{"token":"runtime-test"}`), "credential:"+credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCredential(ctx, store.Credential{ID: credentialID, WorkspaceID: &workspace, Name: "Runtime", Kind: "kubernetes-token", Cipher: cipher}); err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: local.Certificate().Raw})
	if _, err := db.DB.ExecContext(ctx, `UPDATE clusters SET api_server=$1,ca_data=$2,default_credential_id=$3 WHERE id='cluster'`, local.URL, ca, credentialID); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNamespaceBinding(ctx, workspace, "cluster", "dev", &credentialID); err != nil {
		t.Fatal(err)
	}
	app := store.Application{ID: store.NewID(), WorkspaceID: workspace, Name: "Runtime timeout", SourceID: "source", Revision: "main", ManifestPath: ".", Renderer: "yaml", ClusterID: "cluster", Namespaces: []store.NamespaceBinding{{Namespace: "dev"}}, SyncPolicy: "manual", PollSeconds: 300}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertManagedResource(ctx, app.ID, core.Resource{Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "dev", Name: "app"}, UID: "uid", Manifest: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: db, EncryptionKey: key}
	if err := svc.RefreshApplicationHealth(observeCtx, app); !errors.Is(err, context.Canceled) {
		t.Fatalf("observation cancellation lost: %v", err)
	}
	fresh, err := db.ApplicationByID(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.HealthCondition.ObservedAt == nil || fresh.HealthCondition.Status != "Unknown" || fresh.HealthCondition.Reason != "HealthObservationUnavailable" {
		t.Fatalf("timeout left stale health: %+v", fresh.HealthCondition)
	}
}

func TestIntegrationRuntimeObservation(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	app := store.Application{ID: store.NewID(), WorkspaceID: "workspace", Name: "runtime", SourceID: "source", Revision: "main", ManifestPath: ".", Renderer: "yaml", ClusterID: "cluster", SyncPolicy: "manual", PollSeconds: 86400}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,retry_terminal_reason='review_required',last_checked_at=NOW() WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AcquireResourceAction(ctx, app.ID, "owner", time.Minute); err != nil {
		t.Fatal(err)
	}
	due, err := db.DueRuntimeApplications(ctx, 25)
	if err != nil || len(due) != 1 || due[0].ID != app.ID {
		t.Fatalf("paused runtime application not due: %v %v", due, err)
	}
	svc := &Service{Store: db}
	// There are no credentials and the cluster is unreachable. An empty
	// inventory must still publish an observation without cluster access.
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.ApplicationByID(ctx, app.ID)
	if err != nil || fresh.HealthCondition.Reason != "ReconciliationSuspended" || fresh.HealthCondition.ObservedAt == nil {
		t.Fatalf("empty observation: %#v %v", fresh.HealthCondition, err)
	}
	observedAt := *fresh.HealthCondition.ObservedAt
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.ApplicationByID(ctx, app.ID)
	if err != nil || !fresh.HealthCondition.ObservedAt.Equal(observedAt) {
		t.Fatalf("duplicate observation was not coalesced: %#v %v", fresh.HealthCondition, err)
	}
	due, err = db.DueRuntimeApplications(ctx, 25)
	if err != nil || len(due) != 0 {
		t.Fatalf("fresh application should not be due: %v %v", due, err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET health_condition_observed_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(7417,hashtext($1))`, app.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.ApplicationByID(ctx, app.ID)
	if err != nil || time.Since(*fresh.HealthCondition.ObservedAt) < 30*time.Second {
		t.Fatalf("parallel observer bypassed lock: %#v %v", fresh.HealthCondition, err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(7417,hashtext($1))`, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=FALSE WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.ApplicationByID(ctx, app.ID)
	if err != nil || fresh.HealthCondition.Reason != "NoManagedWorkloads" {
		t.Fatalf("unmanaged application = %#v %v", fresh.HealthCondition, err)
	}
}
