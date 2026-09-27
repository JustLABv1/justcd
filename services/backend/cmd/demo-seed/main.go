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
	demoWorkspaceName = "JustCD Demo Workspace"
	demoDescription   = "Demo data only. Git and Kubernetes endpoints use reserved .invalid domains and cannot sync to a real service."
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
		fmt.Println("JustCD demo data added. All demo Git/cluster endpoints use .invalid and are intentionally unreachable.")
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
	err = tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE name=$1 AND description=$2 LIMIT 1`, demoWorkspaceName, demoDescription).Scan(&existing)
	if err == nil {
		created, err := seedHelmTopology(ctx, tx, existing)
		if err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return created, nil
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

	workspaceID := store.NewID()
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

	if _, err := tx.ExecContext(ctx, `INSERT INTO workspaces(id,name,description) VALUES($1,$2,$3)`, workspaceID, demoWorkspaceName, demoDescription); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspaceID, ownerID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES($1,NULL,'DEMO ONLY - shared token (invalid)', 'kubernetes-token',$2)`, globalCredentialID, globalCipher); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES($1,$2,'DEMO ONLY - payments namespace token (invalid)', 'kubernetes-token',$3)`, paymentsCredentialID, workspaceID, paymentsCipher); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO clusters(id,name,api_server,default_credential_id) VALUES($1,'DEMO ONLY - unreachable .invalid cluster','https://kubernetes.justcd-demo.invalid',$2)`, clusterID, globalCredentialID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES($1,$2,'DEMO ONLY - unreachable .invalid Git source','https://git.justcd-demo.invalid/demo/platform.git')`, gitSourceID, workspaceID); err != nil {
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO namespace_bindings(id,workspace_id,cluster_id,namespace,credential_id) VALUES($1,$2,$3,$4,$5)`, store.NewID(), workspaceID, clusterID, namespace.name, namespace.credentialID); err != nil {
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy,poll_seconds,last_checked_at,last_synced_revision,health) VALUES($1,$2,$3,$4,'main',$5,'yaml',$6,$7,$8,86400,$9,$10,$11)`, applicationID, workspaceID, fixture.name, gitSourceID, "manifests/"+fixture.namespace, clusterID, namespaceJSON, fixture.syncPolicy, createdAt, fixture.lastSynced, fixture.health); err != nil {
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
	if _, err := seedHelmTopology(ctx, tx, workspaceID); err != nil {
		return false, err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'demo.seeded','workspace',$2,$3)`, ownerID, workspaceID, jsonBytes(map[string]any{"workspace": demoWorkspaceName, "applications": len(fixtures), "clusterEndpoint": "reserved .invalid"})); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func seedHelmTopology(ctx context.Context, tx *sql.Tx, workspaceID string) (bool, error) {
	const name = "Demo Helm Shop · Topology"
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE workspace_id=$1 AND name=$2`, workspaceID, name).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var clusterID, sourceID, ownerID string
	if err := tx.QueryRowContext(ctx, `SELECT cluster_id,source_id FROM applications WHERE workspace_id=$1 ORDER BY created_at LIMIT 1`, workspaceID).Scan(&clusterID, &sourceID); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM workspace_memberships WHERE workspace_id=$1 AND role='owner' LIMIT 1`, workspaceID).Scan(&ownerID); err != nil {
		return false, err
	}
	appID := store.NewID()
	fixture := helmTopologyFixture(clusterID)
	for i := range fixture.desired {
		fixture.desired[i] = addApplicationOwnership(fixture.desired[i], appID)
	}
	for i := range fixture.live {
		fixture.live[i] = addApplicationOwnership(fixture.live[i], appID)
	}
	namespaces := jsonBytes([]store.NamespaceBinding{{Namespace: "storefront"}})
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy,poll_seconds,last_checked_at,last_synced_revision,health) VALUES($1,$2,$3,$4,'main','charts/demo-shop','helm',$5,$6,'manual',86400,$7,'5555555555555555555555555555555555555555','out_of_sync')`, appID, workspaceID, name, sourceID, clusterID, namespaces, now); err != nil {
		return false, err
	}
	bindings := []core.Binding{{ClusterID: clusterID, Namespace: "storefront", CredentialRef: "demo-topology-credential"}}
	old, oldDesired, current, currentDesired := plansForFixture(appID, fixture, bindings)
	oldID := store.NewID()
	if err := insertPlan(ctx, tx, oldID, old, oldDesired, ownerID, "applied", now.Add(-2*time.Hour), now.Add(-105*time.Minute)); err != nil {
		return false, err
	}
	if err := insertOperation(ctx, tx, appID, &oldID, &ownerID, "succeeded", "Demo history: Helm release deployed successfully.", now.Add(-2*time.Hour), now.Add(-119*time.Minute)); err != nil {
		return false, err
	}
	currentID := store.NewID()
	if err := insertPlan(ctx, tx, currentID, current, currentDesired, ownerID, "current", now, now.Add(15*time.Minute)); err != nil {
		return false, err
	}
	for _, resource := range fixture.live {
		if err := insertManagedResource(ctx, tx, appID, resource); err != nil {
			return false, err
		}
	}
	for _, item := range helmObservedResources(clusterID) {
		labels := jsonBytes(item.Labels)
		owners := jsonBytes(item.OwnerUIDs)
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_resource_observations(application_id,cluster_id,api_version,kind,namespace,name,uid,resource_version,labels,owner_uids,phase,readiness,source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'sample')`, appID, clusterID, item.Identity.APIVersion, item.Identity.Kind, item.Identity.Namespace, item.Identity.Name, item.UID, item.ResourceVersion, labels, owners, item.Phase, item.Readiness); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'demo.seeded','application',$2,$3)`, ownerID, appID, jsonBytes(map[string]any{"application": name, "sampleObservations": true})); err != nil {
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

func helmTopologyFixture(clusterID string) appFixture {
	labels := map[string]any{"app": "shop-web"}
	apiLabels := map[string]any{"app": "shop-api"}
	webSpec := func(replicas int, featureFlags bool) map[string]any {
		config := []any{map[string]any{"configMapRef": map[string]any{"name": "shop-settings"}}}
		if featureFlags {
			config = append(config, map[string]any{"configMapRef": map[string]any{"name": "shop-feature-flags"}})
		}
		return map[string]any{"replicas": replicas, "selector": map[string]any{"matchLabels": labels}, "template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": map[string]any{"serviceAccountName": "shop-runtime", "containers": []any{map[string]any{"name": "web", "image": "example.invalid/shop-web:v2", "envFrom": config}}}}}
	}
	apiSpec := map[string]any{"replicas": 2, "selector": map[string]any{"matchLabels": apiLabels}, "template": map[string]any{"metadata": map[string]any{"labels": apiLabels}, "spec": map[string]any{"serviceAccountName": "shop-runtime", "containers": []any{map[string]any{"name": "api", "image": "example.invalid/shop-api:v1", "envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "shop-runtime"}}}}}, "volumes": []any{map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "shop-data"}}}}}}
	webLive := helmResource(clusterID, "Deployment", "shop-web", "apps/v1", "shop-web-v1", "73", "web-v1", map[string]any{"spec": webSpec(2, false)})
	webDesired := helmResource(clusterID, "Deployment", "shop-web", "apps/v1", "", "", "web-v2", map[string]any{"spec": webSpec(3, true)})
	shared := []core.Resource{
		helmResource(clusterID, "ConfigMap", "shop-settings", "v1", "shop-settings-v1", "11", "config-settings", map[string]any{"data": map[string]any{"THEME": "forest", "PUBLIC_API": "/api"}}),
		helmResource(clusterID, "Deployment", "shop-api", "apps/v1", "shop-api-v1", "48", "api-v1", map[string]any{"spec": apiSpec}),
		helmResource(clusterID, "Service", "shop-web", "v1", "shop-web-svc", "21", "web-service", map[string]any{"spec": map[string]any{"selector": labels, "ports": []any{map[string]any{"port": 80, "targetPort": 8080}}}}),
		helmResource(clusterID, "Service", "shop-api", "v1", "shop-api-svc", "18", "api-service", map[string]any{"spec": map[string]any{"selector": apiLabels, "ports": []any{map[string]any{"port": 8080, "targetPort": 8080}}}}),
		helmResource(clusterID, "Ingress", "shop-public", "networking.k8s.io/v1", "shop-ingress", "12", "ingress", map[string]any{"spec": map[string]any{"rules": []any{map[string]any{"host": "shop.example.invalid", "http": map[string]any{"paths": []any{map[string]any{"path": "/", "pathType": "Prefix", "backend": map[string]any{"service": map[string]any{"name": "shop-web", "port": map[string]any{"number": 80}}}}, map[string]any{"path": "/api", "pathType": "Prefix", "backend": map[string]any{"service": map[string]any{"name": "shop-api", "port": map[string]any{"number": 8080}}}}}}}}}}),
		helmResource(clusterID, "ServiceAccount", "shop-runtime", "v1", "shop-sa", "5", "sa", nil),
		helmResource(clusterID, "Secret", "shop-runtime", "v1", "shop-secret", "8", "secret", map[string]any{"type": "Opaque", "stringData": map[string]any{"DEMO_ONLY": "redacted"}}),
		helmResource(clusterID, "PersistentVolumeClaim", "shop-data", "v1", "shop-pvc", "17", "pvc", map[string]any{"spec": map[string]any{"accessModes": []any{"ReadWriteOnce"}, "resources": map[string]any{"requests": map[string]any{"storage": "1Gi"}}}}),
		helmResource(clusterID, "HorizontalPodAutoscaler", "shop-api", "autoscaling/v2", "shop-hpa", "15", "hpa", map[string]any{"spec": map[string]any{"scaleTargetRef": map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "shop-api"}, "minReplicas": 2, "maxReplicas": 5}}),
	}
	newConfig := helmResource(clusterID, "ConfigMap", "shop-feature-flags", "v1", "", "", "config-new", map[string]any{"data": map[string]any{"CHECKOUT_V2": "enabled"}})
	legacy := helmResource(clusterID, "Job", "shop-migrate-v1", "batch/v1", "shop-old-job", "3", "legacy-job", map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"restartPolicy": "Never", "containers": []any{map[string]any{"name": "migrate", "image": "example.invalid/migrate:v1"}}}}}})
	return appFixture{name: "Demo Helm Shop · Topology", namespace: "storefront", syncPolicy: "manual", health: "out_of_sync", revision: "6666666666666666666666666666666666666666", lastSynced: "5555555555555555555555555555555555555555", desired: append([]core.Resource{webDesired, newConfig}, shared...), live: append([]core.Resource{webLive, legacy}, shared...), planStatus: "current", operation: "succeeded"}
}

func helmResource(clusterID, kind, name, apiVersion, uid, resourceVersion, fingerprintLabel string, fields map[string]any) core.Resource {
	manifest := map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]any{"name": name, "namespace": "storefront", "labels": map[string]any{"app.kubernetes.io/instance": "demo-shop"}}}
	for key, value := range fields {
		manifest[key] = value
	}
	encoded := jsonBytes(manifest)
	return core.Resource{Identity: core.Identity{ClusterID: clusterID, APIVersion: apiVersion, Kind: kind, Namespace: "storefront", Name: name}, Fingerprint: digest(fingerprintLabel + string(encoded)), UID: uid, ResourceVersion: resourceVersion, Manifest: encoded}
}

func helmObservedResources(clusterID string) []store.ObservedResource {
	makeItem := func(kind, name, apiVersion, uid, owner, app, phase, readiness string) store.ObservedResource {
		return store.ObservedResource{Identity: core.Identity{ClusterID: clusterID, APIVersion: apiVersion, Kind: kind, Namespace: "storefront", Name: name}, UID: uid, ResourceVersion: "12", Labels: map[string]string{"app": app}, OwnerUIDs: []string{owner}, Phase: phase, Readiness: readiness, Source: "sample"}
	}
	return []store.ObservedResource{
		makeItem("ReplicaSet", "shop-web-8f6c", "apps/v1", "web-rs-new", "shop-web-v1", "shop-web", "", ""),
		makeItem("ReplicaSet", "shop-web-6b2d", "apps/v1", "web-rs-old", "shop-web-v1", "shop-web", "", ""),
		makeItem("ReplicaSet", "shop-api-9c41", "apps/v1", "api-rs", "shop-api-v1", "shop-api", "", ""),
		makeItem("Pod", "shop-web-8f6c-a12", "v1", "web-pod-1", "web-rs-new", "shop-web", "Running", "Ready"),
		makeItem("Pod", "shop-web-8f6c-b46", "v1", "web-pod-2", "web-rs-new", "shop-web", "Running", "Ready"),
		makeItem("Pod", "shop-web-6b2d-c89", "v1", "web-pod-old", "web-rs-old", "shop-web", "Terminating", "Not ready"),
		makeItem("Pod", "shop-api-9c41-d21", "v1", "api-pod-1", "api-rs", "shop-api", "Running", "Ready"),
		makeItem("Pod", "shop-api-9c41-e52", "v1", "api-pod-2", "api-rs", "shop-api", "Pending", "Not ready"),
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
