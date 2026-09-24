// Command demo-seed adds clearly marked, non-destructive sample records to a
// JustCD database. Its Git and Kubernetes endpoints use the reserved .invalid
// domain, so the generated plans can be inspected but never applied to a real
// cluster.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

const (
	demoProjectName = "JustCD Demo Workspace"
	demoDescription = "Demo data only. Git and Kubernetes endpoints use reserved .invalid domains and cannot sync to a real service."
)

type appFixture struct {
	name          string
	namespace     string
	credentialID  string
	syncPolicy    string
	health        string
	revision      string
	lastSynced    string
	desired       []core.Resource
	live          []core.Resource
	planStatus    string
	operation     string
	operationNote string
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.DB.Close()
	created, err := seed(ctx, db.DB, cfg.EncryptionKey)
	if err != nil {
		log.Fatal(err)
	}
	if created {
		fmt.Println("JustCD demo workspace created. All demo Git/cluster endpoints use .invalid and are intentionally unreachable.")
	} else {
		fmt.Println("JustCD demo workspace already exists; no database rows were changed.")
	}
}

func seed(ctx context.Context, db *sql.DB, encryptionKey []byte) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('justcd-demo-seed-v1'))`); err != nil {
		return false, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE name=$1 AND description=$2 LIMIT 1`, demoProjectName, demoDescription).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var ownerID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE disabled=FALSE ORDER BY is_admin DESC,created_at ASC LIMIT 1`).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errors.New("create or sign in to a JustCD user before seeding demo data")
	}
	if err != nil {
		return false, err
	}

	projectID := store.NewID()
	clusterID := store.NewID()
	globalCredentialID := store.NewID()
	paymentsCredentialID := store.NewID()
	gitSourceID := store.NewID()
	createdAt := time.Now().UTC()
	oldCreatedAt := createdAt.Add(-2 * time.Hour)
	oldExpiry := oldCreatedAt.Add(15 * time.Minute)
	currentExpiry := createdAt.Add(15 * time.Minute)

	globalCipher, err := encryptJSON(encryptionKey, "credential:"+globalCredentialID, map[string]string{"token": "demo-only-invalid-cluster-token"})
	if err != nil {
		return false, err
	}
	paymentsCipher, err := encryptJSON(encryptionKey, "credential:"+paymentsCredentialID, map[string]string{"token": "demo-only-invalid-payments-token"})
	if err != nil {
		return false, err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,description) VALUES($1,$2,$3)`, projectID, demoProjectName, demoDescription); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_memberships(project_id,user_id,role) VALUES($1,$2,'owner')`, projectID, ownerID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credentials(id,project_id,name,kind,secret_cipher) VALUES($1,NULL,'DEMO ONLY - shared token (invalid)', 'kubernetes-token',$2)`, globalCredentialID, globalCipher); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credentials(id,project_id,name,kind,secret_cipher) VALUES($1,$2,'DEMO ONLY - payments namespace token (invalid)', 'kubernetes-token',$3)`, paymentsCredentialID, projectID, paymentsCipher); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO clusters(id,name,api_server,default_credential_id) VALUES($1,'DEMO ONLY - unreachable .invalid cluster','https://kubernetes.justcd-demo.invalid',$2)`, clusterID, globalCredentialID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO git_sources(id,project_id,name,repository_url) VALUES($1,$2,'DEMO ONLY - unreachable .invalid Git source','https://git.justcd-demo.invalid/demo/platform.git')`, gitSourceID, projectID); err != nil {
		return false, err
	}

	namespaces := []struct {
		name         string
		credentialID *string
	}{
		{name: "payments", credentialID: stringPointer(paymentsCredentialID)},
		{name: "storefront"},
		{name: "batch-jobs"},
		{name: "observability"},
		{name: "checkout"},
	}
	for _, namespace := range namespaces {
		if _, err := tx.ExecContext(ctx, `INSERT INTO namespace_bindings(id,project_id,cluster_id,namespace,credential_id) VALUES($1,$2,$3,$4,$5)`, store.NewID(), projectID, clusterID, namespace.name, namespace.credentialID); err != nil {
			return false, err
		}
	}

	fixtures := buildFixtures(clusterID, paymentsCredentialID)
	for _, fixture := range fixtures {
		applicationID := store.NewID()
		for i := range fixture.desired {
			fixture.desired[i] = addApplicationOwnership(fixture.desired[i], applicationID)
		}
		for i := range fixture.live {
			fixture.live[i] = addApplicationOwnership(fixture.live[i], applicationID)
		}
		bindingCredential := fixture.credentialID
		if bindingCredential == "" {
			bindingCredential = globalCredentialID
		}
		applicationBindings := []store.NamespaceBinding{{Namespace: fixture.namespace}}
		if fixture.credentialID != "" {
			applicationBindings[0].CredentialID = stringPointer(fixture.credentialID)
		}
		namespaceJSON, err := json.Marshal(applicationBindings)
		if err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO applications(id,project_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy,poll_seconds,last_checked_at,last_synced_revision,health) VALUES($1,$2,$3,$4,'main',$5,'yaml',$6,$7,$8,86400,$9,$10,$11)`, applicationID, projectID, fixture.name, gitSourceID, "manifests/"+fixture.namespace, clusterID, namespaceJSON, fixture.syncPolicy, createdAt, fixture.lastSynced, fixture.health); err != nil {
			return false, err
		}

		binding := []core.Binding{{ClusterID: clusterID, Namespace: fixture.namespace, CredentialRef: bindingCredential}}
		oldPlan, oldDesired, currentPlan, currentDesired := plansForFixture(applicationID, fixture, binding)
		oldPlanID := store.NewID()
		if err := insertPlan(ctx, tx, oldPlanID, oldPlan, oldDesired, ownerID, "applied", oldCreatedAt, oldExpiry); err != nil {
			return false, err
		}
		if fixture.operation == "succeeded" {
			if err := insertOperation(ctx, tx, applicationID, &oldPlanID, &ownerID, "succeeded", fixture.operationNote, oldCreatedAt, oldCreatedAt.Add(2*time.Minute)); err != nil {
				return false, err
			}
		}

		currentPlanID := store.NewID()
		if err := insertPlan(ctx, tx, currentPlanID, currentPlan, currentDesired, ownerID, fixture.planStatus, createdAt, currentExpiry); err != nil {
			return false, err
		}
		if fixture.operation == "failed" {
			if err := insertOperation(ctx, tx, applicationID, &currentPlanID, &ownerID, "failed", fixture.operationNote, createdAt.Add(-10*time.Minute), createdAt.Add(-9*time.Minute)); err != nil {
				return false, err
			}
		}
		for _, resource := range fixture.live {
			if err := insertManagedResource(ctx, tx, applicationID, resource); err != nil {
				return false, err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'demo.seeded','project',$2,$3)`, ownerID, projectID, jsonBytes(map[string]any{"project": demoProjectName, "applications": len(fixtures), "clusterEndpoint": "reserved .invalid"})); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func buildFixtures(clusterID, paymentsCredentialID string) []appFixture {
	apiOld := resource(clusterID, "payments", "Deployment", "demo-api", "api-live-v1", "api-uid-001", "42", map[string]any{"spec": map[string]any{"replicas": 3}})
	apiService := resource(clusterID, "payments", "Service", "demo-api", "service-live-v1", "api-uid-002", "18", map[string]any{"spec": map[string]any{"type": "ClusterIP", "port": 8080}})
	apiResources := []core.Resource{apiOld, apiService}

	webBefore := resource(clusterID, "storefront", "Deployment", "demo-web", "web-deployment-v1", "web-uid-001", "107", map[string]any{"spec": map[string]any{"replicas": 2}})
	webAfter := resource(clusterID, "storefront", "Deployment", "demo-web", "web-deployment-v2", "", "", map[string]any{"spec": map[string]any{"replicas": 4}})
	secretBefore := resource(clusterID, "storefront", "Secret", "demo-web-runtime", "web-secret-v1", "web-uid-002", "9", map[string]any{"type": "Opaque", "data": map[string]string{"session-token": "bGl2ZS1zZWNyZXQ="}})
	secretAfter := resource(clusterID, "storefront", "Secret", "demo-web-runtime", "web-secret-v2", "", "", map[string]any{"type": "Opaque", "stringData": map[string]string{"session-token": "demo-desired-secret"}})
	newConfig := resource(clusterID, "storefront", "ConfigMap", "demo-web-settings", "web-config-v1", "", "", map[string]any{"data": map[string]string{"THEME": "ocean", "PUBLIC_API": "/api"}})
	webLive := []core.Resource{webBefore, secretBefore}
	webDesired := []core.Resource{webAfter, secretAfter, newConfig}

	worker := resource(clusterID, "batch-jobs", "Deployment", "legacy-worker", "worker-live-v1", "worker-uid-001", "31", map[string]any{"spec": map[string]any{"replicas": 1}})

	metricsBefore := resource(clusterID, "observability", "ConfigMap", "demo-scrape-config", "metrics-config-v1", "metrics-uid-001", "54", map[string]any{"data": map[string]string{"scrape_interval": "60s", "retention": "7d"}})
	metricsAfter := resource(clusterID, "observability", "ConfigMap", "demo-scrape-config", "metrics-config-v2", "", "", map[string]any{"data": map[string]string{"scrape_interval": "30s", "retention": "14d"}})

	checkout := resource(clusterID, "checkout", "Deployment", "demo-checkout", "checkout-desired-v1", "", "", map[string]any{"spec": map[string]any{"replicas": 2}})

	return []appFixture{
		{
			name: "Demo API · Synced", namespace: "payments", credentialID: paymentsCredentialID,
			syncPolicy: "manual", health: "synced", revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", lastSynced: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			desired: apiResources, live: apiResources, planStatus: "applied", operation: "succeeded", operationNote: "Demo history: last sync completed successfully.",
		},
		{
			name: "Demo Web · Drifted", namespace: "storefront", syncPolicy: "manual", health: "out_of_sync",
			revision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", lastSynced: "1111111111111111111111111111111111111111",
			desired: webDesired, live: webLive, planStatus: "current", operation: "succeeded", operationNote: "Demo history: previous sync completed; Git has changed since then.",
		},
		{
			name: "Demo Worker · Deletion approval", namespace: "batch-jobs", syncPolicy: "manual", health: "deletion_pending",
			revision: "cccccccccccccccccccccccccccccccccccccccc", lastSynced: "2222222222222222222222222222222222222222",
			desired: []core.Resource{}, live: []core.Resource{worker}, planStatus: "current", operation: "succeeded", operationNote: "Demo history: previous sync completed; Git now removes a managed worker.",
		},
		{
			name: "Demo Metrics · Auto-safe", namespace: "observability", syncPolicy: "auto-safe", health: "out_of_sync",
			revision: "dddddddddddddddddddddddddddddddddddddddd", lastSynced: "3333333333333333333333333333333333333333",
			desired: []core.Resource{metricsAfter}, live: []core.Resource{metricsBefore}, planStatus: "current",
		},
		{
			name: "Demo Checkout · Failed sync", namespace: "checkout", syncPolicy: "manual", health: "degraded",
			revision: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", lastSynced: "4444444444444444444444444444444444444444",
			desired: []core.Resource{checkout}, live: []core.Resource{}, planStatus: "failed", operation: "failed", operationNote: "Demo history: a previous sync failed; inspect the failed operation state.",
		},
	}
}

func plansForFixture(applicationID string, fixture appFixture, bindings []core.Binding) (core.Plan, []core.Resource, core.Plan, []core.Resource) {
	oldDesired := fixture.live
	oldLive := fixture.live
	currentDesired := fixture.desired
	currentLive := fixture.live
	if fixture.name == "Demo Checkout · Failed sync" {
		oldDesired = []core.Resource{}
		oldLive = []core.Resource{}
	}
	old, err := core.BuildPlan(applicationID, "9999999999999999999999999999999999999999", bindings, oldDesired, oldLive)
	if err != nil {
		panic(err)
	}
	current, err := core.BuildPlan(applicationID, fixture.revision, bindings, currentDesired, currentLive)
	if err != nil {
		panic(err)
	}
	return old, oldDesired, current, currentDesired
}

func addApplicationOwnership(resource core.Resource, applicationID string) core.Resource {
	var manifest map[string]any
	if err := json.Unmarshal(resource.Manifest, &manifest); err != nil {
		panic(err)
	}
	metadata, _ := manifest["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	labels, _ := metadata["labels"].(map[string]any)
	if labels == nil {
		labels = map[string]any{}
	}
	labels["justcd.io/application-id"] = applicationID
	metadata["labels"] = labels
	manifest["metadata"] = metadata
	encoded, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	resource.Manifest = encoded
	resource.Fingerprint = digest(resource.Fingerprint + "\x00" + applicationID)
	if resource.UID != "" {
		resource.Owner = applicationID
	}
	return resource
}

func resource(clusterID, namespace, kind, name, fingerprintLabel, uid, resourceVersion string, fields map[string]any) core.Resource {
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels":    map[string]string{"justcd.io/application-id": "fixture-owner"},
		},
	}
	if kind == "Deployment" {
		manifest["apiVersion"] = "apps/v1"
	}
	if kind == "Secret" {
		manifest["apiVersion"] = "v1"
	}
	for key, value := range fields {
		manifest[key] = value
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	identity := core.Identity{ClusterID: clusterID, APIVersion: manifest["apiVersion"].(string), Kind: kind, Namespace: namespace, Name: name}
	return core.Resource{Identity: identity, Fingerprint: digest(fingerprintLabel + string(encoded)), UID: uid, ResourceVersion: resourceVersion, Manifest: encoded}
}

func insertManagedResource(ctx context.Context, tx *sql.Tx, applicationID string, resource core.Resource) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO managed_resources(application_id,cluster_id,api_version,kind,namespace,name,uid,resource_version,manifest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, applicationID, resource.Identity.ClusterID, resource.Identity.APIVersion, resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, resource.UID, resource.ResourceVersion, resource.Manifest)
	return err
}

func insertPlan(ctx context.Context, tx *sql.Tx, id string, plan core.Plan, desired []core.Resource, actorID, status string, createdAt, expiresAt time.Time) error {
	bindings, err := json.Marshal(plan.Bindings)
	if err != nil {
		return err
	}
	changes, err := json.Marshal(plan.Changes)
	if err != nil {
		return err
	}
	desiredJSON, err := json.Marshal(desired)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,bindings,changes,desired,created_by,created_at,expires_at,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, plan.ApplicationID, plan.Revision, plan.Digest, bindings, changes, desiredJSON, actorID, createdAt, expiresAt, status)
	return err
}

func insertOperation(ctx context.Context, tx *sql.Tx, applicationID string, planID, actorID *string, status, message string, startedAt, finishedAt time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,actor_id,status,message,started_at,finished_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, store.NewID(), applicationID, planID, actorID, status, message, startedAt, finishedAt)
	return err
}

func encryptJSON(key []byte, purpose string, value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return security.Encrypt(key, encoded, purpose)
}

func jsonBytes(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func stringPointer(value string) *string { return &value }
